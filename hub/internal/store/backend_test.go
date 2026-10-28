package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backend opens a fresh, empty store of one kind.
type backend struct {
	name string
	open func(t *testing.T) *Store
}

// new opens a store whose clock stands at now.
func (b backend) new(t *testing.T, now time.Time) *Store {
	t.Helper()
	st := b.open(t)
	st.Now = func() time.Time { return now }
	return st
}

func sqliteBackend() backend {
	return backend{name: "sqlite", open: func(t *testing.T) *Store {
		t.Helper()
		st, err := Open(filepath.Join(t.TempDir(), "hub.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return st
	}}
}

// pgDSN reads the DSN from the file JOLITZ_HUB_TEST_PG names; ""
// when the variable is unset. A set variable whose file cannot
// be read fails the test: someone asked for PostgreSQL.
func pgDSN(t *testing.T) string {
	t.Helper()
	f := os.Getenv("JOLITZ_HUB_TEST_PG")
	if f == "" {
		return ""
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("JOLITZ_HUB_TEST_PG: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// pgBackend gives every store its own schema in the test
// database and drops it afterwards.
func pgBackend(dsn string) backend {
	return backend{name: "postgres", open: func(t *testing.T) *Store {
		t.Helper()
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Close() })
		raw := make([]byte, 6)
		rand.Read(raw)
		schema := "hubtest_" + hex.EncodeToString(raw)
		if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
		st, err := openPostgres(dsn, schema)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return st
	}}
}

// eachBackend runs fn against SQLite, and against PostgreSQL
// when JOLITZ_HUB_TEST_PG names a DSN file.
func eachBackend(t *testing.T, fn func(t *testing.T, b backend)) {
	t.Helper()
	bs := []backend{sqliteBackend()}
	if dsn := pgDSN(t); dsn != "" {
		bs = append(bs, pgBackend(dsn))
	}
	for _, b := range bs {
		t.Run(b.name, func(t *testing.T) { fn(t, b) })
	}
	if len(bs) == 1 {
		t.Run("postgres", func(t *testing.T) { t.Skip("JOLITZ_HUB_TEST_PG not set") })
	}
}

// newStore is a SQLite store for the tests that are about
// SQLite alone.
func newStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	return sqliteBackend().new(t, now)
}
