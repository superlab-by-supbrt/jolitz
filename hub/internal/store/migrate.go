package store

import (
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFiles embed.FS

// migration is one numbered schema step of one backend. Both
// backends reach the same tables at the same number; how they
// get there may differ (SQLite carries databases from before
// the numbering, PostgreSQL starts at the final shape).
type migration struct {
	Version int
	Name    string
	SQL     string
}

func migrations(pg bool) ([]migration, error) {
	dir := "migrations/sqlite"
	if pg {
		dir = "migrations/postgres"
	}
	ents, err := migrationFiles.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range ents {
		num, name, ok := strings.Cut(strings.TrimSuffix(e.Name(), ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v < 1 {
			return nil, fmt.Errorf("migration file %s: want NNNN_name.sql", e.Name())
		}
		b, err := migrationFiles.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{Version: v, Name: name, SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations of %s: %d missing", dir, i+1)
		}
	}
	return out, nil
}

// SchemaVersion is the newest schema the binary knows; both
// backends reach it.
func SchemaVersion() int {
	a, _ := migrations(false)
	return len(a)
}

// sqliteFixups run after the SQL of a SQLite migration, inside
// its transaction, for what SQL alone cannot say.
var sqliteFixups = map[int]func(*txn) error{1: sqlitePhaseOne}

// sqlitePhaseOne carries a phase-1 database forward: its files
// table has no version column. Every stored file becomes version
// 1, its row the first entry of its history.
func sqlitePhaseOne(t *txn) error {
	has := false
	rows, err := t.Query(`PRAGMA table_info(files)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "version" {
			has = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !has {
		if _, err := t.Exec(`ALTER TABLE files ADD COLUMN version INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
	}
	_, err = t.Exec(`INSERT OR IGNORE INTO file_versions(path, version, body, sha256, size, updated_at, updated_by)
		SELECT path, version, body, sha256, size, updated_at, updated_by FROM files`)
	return err
}

// pgMigrationLock is the advisory lock key two hubs starting
// against one PostgreSQL database serialise their migrations on.
const pgMigrationLock = 0x6a6f6c69 // "joli"

// migrate applies every migration the database lacks, each in
// its own transaction together with its schema_migrations row.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	ms, err := migrations(s.db.pg)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if err := s.apply(m); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

func (s *Store) apply(m migration) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if t.pg {
		if _, err := t.Exec(`SELECT pg_advisory_xact_lock(?)`, int64(pgMigrationLock)); err != nil {
			return err
		}
	}
	var n int
	if err := t.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := t.Exec(m.SQL); err != nil {
		return err
	}
	if fix := sqliteFixups[m.Version]; fix != nil && !t.pg {
		if err := fix(t); err != nil {
			return err
		}
	}
	if _, err := t.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?,?,?)`,
		m.Version, m.Name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return t.Commit()
}

// Version reports the schema version the database is at.
func (s *Store) Version() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT coalesce(max(version), 0) FROM schema_migrations`).Scan(&v)
	return v, err
}

// Backend names the database behind the store: "sqlite" or
// "postgres".
func (s *Store) Backend() string {
	if s.db.pg {
		return "postgres"
	}
	return "sqlite"
}
