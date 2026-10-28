// Package store is the hub's SQLite database: users, the raw
// memory files, the tables derived from them on every write
// (hosts, changes, todos, links, audits, aliases), what only
// the hub knows (sessions, claims, guard denials) and what its
// jobs keep (the endoflife.date cache, the last run of each
// job). Clients never see this schema; they speak to
// internal/api.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
)

var (
	// ErrNotFound: no such row.
	ErrNotFound = errors.New("not found")
	// ErrMismatch: the If-Match sha256 is not the stored one.
	ErrMismatch = errors.New("sha256 mismatch")
	// ErrExists: a user of that name exists.
	ErrExists = errors.New("already exists")
	// ErrInvalid: a name, role or path the store does not take.
	ErrInvalid = errors.New("invalid")
	// ErrConflict: the host is claimed by someone else.
	ErrConflict = errors.New("claimed by someone else")
)

// Roles in ascending order of power.
const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"
)

// RoleAtLeast reports whether role has at least the power of min.
func RoleAtLeast(role, min string) bool {
	return rank(role) >= rank(min) && rank(role) > 0
}

func rank(r string) int {
	switch r {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	}
	return 0
}

// Stale and Closed are the heartbeat ages after which an open
// session is shown as stale and closed for good.
const (
	Stale  = 10 * time.Minute
	Closed = 60 * time.Minute
)

// Store wraps the database. Now is the clock; tests replace it.
type Store struct {
	db  *sql.DB
	Now func() time.Time
}

// Open opens or creates the database at path. ":memory:" gives a
// private in-memory database for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db, Now: func() time.Time { return time.Now().UTC() }}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS users(
  name TEXT PRIMARY KEY, token_sha256 TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files(
  path TEXT PRIMARY KEY, body BLOB NOT NULL, sha256 TEXT NOT NULL,
  size INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS file_versions(
  path TEXT NOT NULL, version INTEGER NOT NULL, body BLOB NOT NULL,
  sha256 TEXT NOT NULL, size INTEGER NOT NULL, updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL, PRIMARY KEY(path, version));
CREATE TABLE IF NOT EXISTS hosts(
  name TEXT PRIMARY KEY, os TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '',
  distro TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '', services TEXT NOT NULL DEFAULT '',
  guest TEXT NOT NULL DEFAULT '', ssh_port TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT '', last_connected TEXT NOT NULL DEFAULT '',
  product TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS aliases(alias TEXT PRIMARY KEY, host TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS changes(
  id INTEGER PRIMARY KEY AUTOINCREMENT, host TEXT NOT NULL, date TEXT NOT NULL,
  time TEXT NOT NULL, who TEXT NOT NULL, headline TEXT NOT NULL,
  detail TEXT NOT NULL, rollback TEXT NOT NULL, flags TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS changes_host ON changes(host);
CREATE INDEX IF NOT EXISTS changes_date ON changes(date, time);
CREATE TABLE IF NOT EXISTS todos(host TEXT NOT NULL, text TEXT NOT NULL, open INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS links(src TEXT NOT NULL, dst TEXT NOT NULL, label TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audits(snapshot TEXT NOT NULL, host TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS audits_snapshot ON audits(snapshot);
CREATE TABLE IF NOT EXISTS sessions(
  id TEXT PRIMARY KEY, user TEXT NOT NULL, host TEXT NOT NULL,
  workstation TEXT NOT NULL, started_at TEXT NOT NULL,
  last_beat TEXT NOT NULL, ended_at TEXT);
CREATE TABLE IF NOT EXISTS claims(host TEXT PRIMARY KEY, user TEXT NOT NULL, note TEXT NOT NULL, since TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS guard(id INTEGER PRIMARY KEY AUTOINCREMENT, user TEXT NOT NULL, at TEXT NOT NULL, reason TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS eol(product TEXT PRIMARY KEY, fetched TEXT NOT NULL, body BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jobs(name TEXT PRIMARY KEY, last_run TEXT NOT NULL, ok INTEGER NOT NULL, result TEXT NOT NULL);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// A phase-1 database has files without a version column.
	// Every stored file becomes version 1, its row the first
	// entry of its history.
	has := false
	if err := s.each(`PRAGMA table_info(files)`, func(sc scanner) error {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := sc.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "version" {
			has = true
		}
		return nil
	}); err != nil {
		return err
	}
	if !has {
		if _, err := s.db.Exec(`ALTER TABLE files ADD COLUMN version INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO file_versions(path, version, body, sha256, size, updated_at, updated_by)
		SELECT path, version, body, sha256, size, updated_at, updated_by FROM files`)
	return err
}

func (s *Store) now() string { return s.Now().UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// ---- users -------------------------------------------------

// User is a row of users, without the token hash.
type User struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

var userName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func newToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// AddUser creates a user and returns its token, the only time it
// is visible.
func (s *Store) AddUser(name, role string) (string, error) {
	if !userName.MatchString(name) {
		return "", fmt.Errorf("%w: user name %q", ErrInvalid, name)
	}
	if rank(role) == 0 {
		return "", fmt.Errorf("%w: role %q (viewer, operator or admin)", ErrInvalid, role)
	}
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO users(name, token_sha256, role, created_at) VALUES(?,?,?,?)`,
		name, hash, role, s.now())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("%w: user %s", ErrExists, name)
	}
	return token, nil
}

// RotateToken replaces the token of a user and returns the new one.
func (s *Store) RotateToken(name string) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	res, err := s.db.Exec(`UPDATE users SET token_sha256 = ? WHERE name = ?`, hash, name)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

// RemoveUser deletes a user; its sessions end, its claims stay.
func (s *Store) RemoveUser(name string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE user = ? AND ended_at IS NULL`, s.now(), name)
	return err
}

// ListUsers returns every user, by name.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT name, role, created_at FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var at string
		if err := rows.Scan(&u.Name, &u.Role, &at); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTime(at)
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserByToken resolves a bearer token; nil when unknown.
func (s *Store) UserByToken(token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	var u User
	var at string
	err := s.db.QueryRow(`SELECT name, role, created_at FROM users WHERE token_sha256 = ?`, hashToken(token)).
		Scan(&u.Name, &u.Role, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTime(at)
	return &u, nil
}

// ---- files -------------------------------------------------

// Kind classifies an allowed path.
type Kind int

// The allowed file kinds.
const (
	KindInvalid   Kind = iota
	KindMemory         // servers/<host>/memory.md
	KindChangelog      // servers/<host>/changelog.log
	KindTodo           // servers/<host>/todo.md
	KindRules          // servers/<host>/rules.md
	KindAlias          // servers/<alias>, body = canonical host
	KindNetwork        // network.md
	KindAudit          // audits/<YYYY-MM-DD-HHMM>.json
)

var auditName = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{4}\.json$`)

// Classify tells what an allowed path is and which host or
// snapshot it belongs to. Anything else is KindInvalid.
func Classify(path string) (kind Kind, host string) {
	switch {
	case path == "network.md":
		return KindNetwork, ""
	case strings.HasPrefix(path, "audits/"):
		name := path[len("audits/"):]
		if auditName.MatchString(name) {
			return KindAudit, strings.TrimSuffix(name, ".json")
		}
	case strings.HasPrefix(path, "servers/"):
		rest := path[len("servers/"):]
		h, file, hasSlash := strings.Cut(rest, "/")
		if !parse.ValidHost(h) {
			return KindInvalid, ""
		}
		if !hasSlash {
			return KindAlias, h
		}
		switch file {
		case "memory.md":
			return KindMemory, h
		case "changelog.log":
			return KindChangelog, h
		case "todo.md":
			return KindTodo, h
		case "rules.md":
			return KindRules, h
		}
	}
	return KindInvalid, ""
}

// FileInfo is one row of the manifest. Version counts the
// writes of a path from 1; every write keeps the previous body
// in the history.
type FileInfo struct {
	Path      string    `json:"path"`
	Version   int       `json:"version"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// SHA256 is the hex digest the manifest and If-Match use.
func SHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ListFiles returns the manifest, by path.
func (s *Store) ListFiles() ([]FileInfo, error) {
	rows, err := s.db.Query(`SELECT path, version, sha256, size, updated_at, updated_by FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileInfo{}
	for rows.Next() {
		var f FileInfo
		var at string
		if err := rows.Scan(&f.Path, &f.Version, &f.SHA256, &f.Size, &at, &f.UpdatedBy); err != nil {
			return nil, err
		}
		f.UpdatedAt = parseTime(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func getFile(q querier, path string) ([]byte, *FileInfo, error) {
	var body []byte
	var f FileInfo
	var at string
	err := q.QueryRow(`SELECT path, version, body, sha256, size, updated_at, updated_by FROM files WHERE path = ?`, path).
		Scan(&f.Path, &f.Version, &body, &f.SHA256, &f.Size, &at, &f.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f.UpdatedAt = parseTime(at)
	if body == nil {
		body = []byte{}
	}
	return body, &f, nil
}

// GetFile returns a file's bytes and manifest row.
func (s *Store) GetFile(path string) ([]byte, *FileInfo, error) {
	return getFile(s.db, path)
}

// GetFileVersion returns one version of a file from its
// history, current or old; ErrNotFound when the path never had
// that version.
func (s *Store) GetFileVersion(path string, version int) ([]byte, *FileInfo, error) {
	var body []byte
	f := FileInfo{Path: path, Version: version}
	var at string
	err := s.db.QueryRow(`SELECT body, sha256, size, updated_at, updated_by FROM file_versions WHERE path = ? AND version = ?`,
		path, version).Scan(&body, &f.SHA256, &f.Size, &at, &f.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f.UpdatedAt = parseTime(at)
	if body == nil {
		body = []byte{}
	}
	return body, &f, nil
}

// FileHistory lists the versions of a path, newest first. A
// deleted file keeps its history; an unknown path gives an
// empty list.
func (s *Store) FileHistory(path string) ([]FileInfo, error) {
	out := []FileInfo{}
	err := s.each(`SELECT version, sha256, size, updated_at, updated_by FROM file_versions WHERE path = ? ORDER BY version DESC`,
		func(sc scanner) error {
			f := FileInfo{Path: path}
			var at string
			if err := sc.Scan(&f.Version, &f.SHA256, &f.Size, &at, &f.UpdatedBy); err != nil {
				return err
			}
			f.UpdatedAt = parseTime(at)
			out = append(out, f)
			return nil
		}, path)
	return out, err
}

// checkPath validates a path and body for PutFile.
func checkPath(path string, body []byte) (Kind, string, error) {
	kind, host := Classify(path)
	if kind == KindInvalid {
		return kind, "", fmt.Errorf("%w: path %q", ErrInvalid, path)
	}
	if kind == KindAlias && parse.Alias(body) == "" {
		return kind, "", fmt.Errorf("%w: an alias file holds one canonical host name", ErrInvalid)
	}
	if kind == KindAudit {
		if _, _, err := parse.Audit(body); err != nil {
			return kind, "", fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	return kind, host, nil
}

// PutFile stores a file as a new version and rebuilds the tables
// derived from it. ifMatch, when not empty, must equal the
// stored sha256 (or the file must not exist when ifMatch is
// "-"); otherwise ErrMismatch and nothing changes. An alias body
// that is not a host name is ErrInvalid.
func (s *Store) PutFile(path string, body []byte, by, ifMatch string) (*FileInfo, error) {
	kind, host, err := checkPath(path, body)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	f, err := s.putFile(tx, kind, host, path, body, by, ifMatch)
	if err != nil {
		return nil, err
	}
	return f, tx.Commit()
}

// UpdateFile rewrites a file in one transaction: edit gets the
// current body (nil when the file does not exist) and returns
// the new one; an error from edit is returned as it is. The
// write is a new version by user by, as PutFile makes it.
func (s *Store) UpdateFile(path, by string, edit func(old []byte) ([]byte, error)) (*FileInfo, error) {
	if kind, _ := Classify(path); kind == KindInvalid {
		return nil, fmt.Errorf("%w: path %q", ErrInvalid, path)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	old, cur, err := getFile(tx, path)
	ifMatch := "-"
	switch {
	case errors.Is(err, ErrNotFound):
		old = nil
	case err != nil:
		return nil, err
	default:
		ifMatch = cur.SHA256
	}
	body, err := edit(old)
	if err != nil {
		return nil, err
	}
	kind, host, err := checkPath(path, body)
	if err != nil {
		return nil, err
	}
	f, err := s.putFile(tx, kind, host, path, body, by, ifMatch)
	if err != nil {
		return nil, err
	}
	return f, tx.Commit()
}

func (s *Store) putFile(tx *sql.Tx, kind Kind, host, path string, body []byte, by, ifMatch string) (*FileInfo, error) {
	var cur string
	err := tx.QueryRow(`SELECT sha256 FROM files WHERE path = ?`, path).Scan(&cur)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if ifMatch != "" {
		if ifMatch == "-" && exists || ifMatch != "-" && ifMatch != cur {
			return nil, ErrMismatch
		}
	}
	// The counter continues after a delete, so a version number
	// names one body for the life of the hub.
	var version int
	if err := tx.QueryRow(`SELECT coalesce(max(version), 0) + 1 FROM file_versions WHERE path = ?`, path).Scan(&version); err != nil {
		return nil, err
	}
	now := s.now()
	f := &FileInfo{Path: path, Version: version, SHA256: SHA256(body), Size: int64(len(body)), UpdatedAt: parseTime(now), UpdatedBy: by}
	if _, err := tx.Exec(`INSERT INTO files(path, body, sha256, size, updated_at, updated_by, version) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET body = excluded.body, sha256 = excluded.sha256, size = excluded.size,
		updated_at = excluded.updated_at, updated_by = excluded.updated_by, version = excluded.version`,
		path, body, f.SHA256, f.Size, now, by, version); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO file_versions(path, version, body, sha256, size, updated_at, updated_by) VALUES(?,?,?,?,?,?,?)`,
		path, version, body, f.SHA256, f.Size, now, by); err != nil {
		return nil, err
	}
	return f, derive(tx, kind, host, body, now)
}

// PruneFileVersions deletes history entries written before t.
// The current version of every stored file stays, whatever its
// age. A deleted file has no current version: once its newest
// version is older than t, its whole history goes, so a delete
// does not keep the content forever.
func (s *Store) PruneFileVersions(t time.Time) (int64, error) {
	cut := t.UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM file_versions WHERE updated_at < ?
		AND (version < (SELECT max(version) FROM file_versions v WHERE v.path = file_versions.path)
		  OR path NOT IN (SELECT path FROM files))`, cut)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteFile removes a file and what was derived from it.
func (s *Store) DeleteFile(path string) error {
	kind, host := Classify(path)
	if kind == KindInvalid {
		return fmt.Errorf("%w: path %q", ErrInvalid, path)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM files WHERE path = ?`, path)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := derive(tx, kind, host, nil, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}

// derive rebuilds the rows one file feeds. A nil body means the
// file is gone.
func derive(tx *sql.Tx, kind Kind, host string, body []byte, now string) error {
	hostFile := kind == KindMemory || kind == KindChangelog || kind == KindTodo || kind == KindRules
	if hostFile && body != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO hosts(name, updated_at) VALUES(?,?)`, host, now); err != nil {
			return err
		}
	}
	switch kind {
	case KindMemory:
		h := parse.Memory(body)
		_, err := tx.Exec(`UPDATE hosts SET os=?, ip=?, distro=?, tags=?, owner=?, services=?, guest=?,
			ssh_port=?, mode=?, last_connected=?, product=?, version=?, updated_at=? WHERE name=?`,
			h.OS, h.IP, h.Distro, strings.Join(h.Tags, " "), h.Owner, h.Fields["Services"],
			strings.Join(h.Guest, " "), h.SSHPort, h.Mode, h.LastConnected, h.Product, h.Version, now, host)
		if err != nil {
			return err
		}
	case KindChangelog:
		if _, err := tx.Exec(`DELETE FROM changes WHERE host = ?`, host); err != nil {
			return err
		}
		for _, c := range parse.Changelog(host, body) {
			if _, err := tx.Exec(`INSERT INTO changes(host, date, time, who, headline, detail, rollback, flags) VALUES(?,?,?,?,?,?,?,?)`,
				host, c.Date, c.Time, c.Who, c.Headline, strings.Join(c.Detail, "\x1f"), c.Rollback, c.Flags); err != nil {
				return err
			}
		}
	case KindTodo:
		if _, err := tx.Exec(`DELETE FROM todos WHERE host = ?`, host); err != nil {
			return err
		}
		for _, t := range parse.Todos(body) {
			open := 0
			if t.Open {
				open = 1
			}
			if _, err := tx.Exec(`INSERT INTO todos(host, text, open) VALUES(?,?,?)`, host, t.Text, open); err != nil {
				return err
			}
		}
	case KindAlias:
		if body == nil {
			_, err := tx.Exec(`DELETE FROM aliases WHERE alias = ?`, host)
			return err
		}
		if _, err := tx.Exec(`INSERT INTO aliases(alias, host) VALUES(?,?) ON CONFLICT(alias) DO UPDATE SET host = excluded.host`,
			host, parse.Alias(body)); err != nil {
			return err
		}
	case KindNetwork:
		if _, err := tx.Exec(`DELETE FROM links`); err != nil {
			return err
		}
		for _, l := range parse.Network(body) {
			if _, err := tx.Exec(`INSERT INTO links(src, dst, label) VALUES(?,?,?)`, l.Src, l.Dst, l.Label); err != nil {
				return err
			}
		}
	case KindAudit:
		if _, err := tx.Exec(`DELETE FROM audits WHERE snapshot = ?`, host); err != nil {
			return err
		}
		if body != nil {
			_, vals, err := parse.Audit(body)
			if err != nil {
				return err
			}
			for _, v := range vals {
				if _, err := tx.Exec(`INSERT INTO audits(snapshot, host, key, value) VALUES(?,?,?,?)`, host, v.Host, v.Key, v.Value); err != nil {
					return err
				}
			}
		}
	}
	if hostFile && body == nil {
		// Without files left the host disappears from the fleet.
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM files WHERE path LIKE ?`, "servers/"+host+"/%").Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			for _, q := range []string{`DELETE FROM hosts WHERE name = ?`, `DELETE FROM changes WHERE host = ?`, `DELETE FROM todos WHERE host = ?`} {
				if _, err := tx.Exec(q, host); err != nil {
					return err
				}
			}
		} else if kind == KindMemory {
			if _, err := tx.Exec(`UPDATE hosts SET os='', ip='', distro='', tags='', owner='', services='', guest='',
				ssh_port='', mode='', last_connected='', product='', version='', updated_at=? WHERE name=?`, now, host); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- hosts -------------------------------------------------

// Host is the fleet view of one server: the fields of
// overview.json plus what the hub knows (claim, sessions).
type Host struct {
	Name              string            `json:"name"`
	OS                string            `json:"os"`
	IP                string            `json:"ip"`
	DistroFamily      string            `json:"distro_family"`
	LastConnected     string            `json:"last_connected"`
	LastConnectedDays int               `json:"last_connected_days"`
	Tags              []string          `json:"tags"`
	SSHPort           string            `json:"ssh_port"`
	Mode              string            `json:"mode"`
	Rules             bool              `json:"rules"`
	Todos             int               `json:"todos"`
	LastChange        string            `json:"last_change"`
	Product           string            `json:"product"`
	Version           string            `json:"version"`
	Owner             string            `json:"owner"`
	Aliases           []string          `json:"aliases"`
	Services          []parse.Service   `json:"services"`
	Guest             []string          `json:"guest"`
	Audit             map[string]string `json:"audit"`
	Claim             *Claim            `json:"claim"`
	Sessions          []Session         `json:"sessions"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// HostDetail is one host with everything its page shows.
type HostDetail struct {
	Host
	Changes   []parse.Change     `json:"changes"`
	TodoItems []parse.Todo       `json:"todo_items"`
	RulesMD   string             `json:"rules_md"`
	MemoryMD  string             `json:"memory_md"`
	AuditDate string             `json:"audit_date"`
	AuditRows []parse.AuditValue `json:"audit_rows"`
}

// Days since a YYYY-MM-DD date, -1 when it is no such date.
func (s *Store) daysSince(d string) int {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return -1
	}
	now := s.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return int(today.Sub(t).Hours() / 24)
}

func (s *Store) scanHosts(where string, args ...any) ([]Host, error) {
	rows, err := s.db.Query(`SELECT name, os, ip, distro, tags, owner, services, guest, ssh_port, mode,
		last_connected, product, version, updated_at FROM hosts `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Host
	for rows.Next() {
		var h Host
		var tags, services, guest, at string
		if err := rows.Scan(&h.Name, &h.OS, &h.IP, &h.DistroFamily, &tags, &h.Owner, &services, &guest,
			&h.SSHPort, &h.Mode, &h.LastConnected, &h.Product, &h.Version, &at); err != nil {
			return nil, err
		}
		h.Tags = nonNil(strings.Fields(tags))
		h.Guest = nonNil(strings.Fields(guest))
		h.Services = parse.Services(services)
		if h.Services == nil {
			h.Services = []parse.Service{}
		}
		h.Aliases = []string{}
		h.Audit = map[string]string{}
		h.Sessions = []Session{}
		h.UpdatedAt = parseTime(at)
		h.LastConnectedDays = s.daysSince(h.LastConnected)
		out = append(out, h)
	}
	return out, rows.Err()
}

func nonNil(a []string) []string {
	if a == nil {
		return []string{}
	}
	return a
}

// Hosts returns the fleet.
func (s *Store) Hosts() ([]Host, error) {
	hosts, err := s.scanHosts("")
	if err != nil {
		return nil, err
	}
	if hosts == nil {
		hosts = []Host{}
	}
	if err := s.decorate(hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

// decorate fills the counters and hub fields of hosts.
func (s *Store) decorate(hosts []Host) error {
	idx := map[string]*Host{}
	for i := range hosts {
		idx[hosts[i].Name] = &hosts[i]
	}
	if err := s.each(`SELECT host, count(*) FROM todos WHERE open = 1 GROUP BY host`, func(sc scanner) error {
		var h string
		var n int
		if err := sc.Scan(&h, &n); err != nil {
			return err
		}
		if x := idx[h]; x != nil {
			x.Todos = n
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.each(`SELECT host, max(date) FROM changes GROUP BY host`, func(sc scanner) error {
		var h, d string
		if err := sc.Scan(&h, &d); err != nil {
			return err
		}
		if x := idx[h]; x != nil {
			x.LastChange = d
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.each(`SELECT path FROM files WHERE path LIKE 'servers/%/rules.md'`, func(sc scanner) error {
		var p string
		if err := sc.Scan(&p); err != nil {
			return err
		}
		_, h := Classify(p)
		if x := idx[h]; x != nil {
			x.Rules = true
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.each(`SELECT alias, host FROM aliases ORDER BY alias`, func(sc scanner) error {
		var a, h string
		if err := sc.Scan(&a, &h); err != nil {
			return err
		}
		if x := idx[h]; x != nil {
			x.Aliases = append(x.Aliases, a)
		}
		return nil
	}); err != nil {
		return err
	}
	snap, err := s.newestSnapshot()
	if err != nil {
		return err
	}
	if snap != "" {
		vals, err := s.AuditValues(snap, "")
		if err != nil {
			return err
		}
		for _, v := range vals {
			if x := idx[v.Host]; x != nil {
				x.Audit[v.Key] = v.Value
			}
		}
	}
	claims, err := s.Claims()
	if err != nil {
		return err
	}
	for i := range claims {
		if x := idx[claims[i].Host]; x != nil {
			c := claims[i]
			x.Claim = &c
		}
	}
	sessions, err := s.OpenSessions()
	if err != nil {
		return err
	}
	for _, se := range sessions {
		if x := idx[se.Host]; x != nil {
			x.Sessions = append(x.Sessions, se)
		}
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func (s *Store) each(q string, fn func(scanner) error, args ...any) error {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Host returns one host with its page data.
func (s *Store) Host(name string) (*HostDetail, error) {
	hosts, err := s.scanHosts("WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, ErrNotFound
	}
	if err := s.decorate(hosts); err != nil {
		return nil, err
	}
	d := &HostDetail{Host: hosts[0], TodoItems: []parse.Todo{}, AuditRows: []parse.AuditValue{}}
	d.Changes, err = s.Changes(ChangeFilter{Host: name})
	if err != nil {
		return nil, err
	}
	if err := s.each(`SELECT text, open FROM todos WHERE host = ? ORDER BY rowid`, func(sc scanner) error {
		var t parse.Todo
		var open int
		if err := sc.Scan(&t.Text, &open); err != nil {
			return err
		}
		t.Open = open == 1
		d.TodoItems = append(d.TodoItems, t)
		return nil
	}, name); err != nil {
		return nil, err
	}
	if b, _, err := s.GetFile("servers/" + name + "/rules.md"); err == nil {
		d.RulesMD = string(b)
	}
	if b, _, err := s.GetFile("servers/" + name + "/memory.md"); err == nil {
		d.MemoryMD = string(b)
	}
	snap, err := s.newestSnapshot()
	if err != nil {
		return nil, err
	}
	if snap != "" {
		d.AuditDate = SnapshotDate(snap)
		d.AuditRows, err = s.AuditValues(snap, name)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Handover is what the next person on a host needs first: who
// holds it, who is on it, what is open, what runs, and the last
// changes with their way back.
type Handover struct {
	Host     string         `json:"host"`
	Claim    *Claim         `json:"claim"`
	Sessions []Session      `json:"sessions"`
	Todos    []string       `json:"todos"`
	Jobs     []string       `json:"jobs"`
	Changes  []parse.Change `json:"changes"`
}

// JobsDir is what a to-do line names when it records a running
// job (rules/ssh-connections.md).
const JobsDir = ".jolitz/jobs/"

// Handover builds the handover block of a host; ErrNotFound for
// a host without files.
func (s *Store) Handover(name string) (*Handover, error) {
	hosts, err := s.scanHosts("WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, ErrNotFound
	}
	h := &Handover{Host: name, Sessions: []Session{}, Todos: []string{}, Jobs: []string{}}
	if c, err := s.Claim(name); err == nil {
		h.Claim = c
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	sessions, err := s.OpenSessions()
	if err != nil {
		return nil, err
	}
	for _, se := range sessions {
		if se.Host == name {
			h.Sessions = append(h.Sessions, se)
		}
	}
	todos, err := s.Todos(name)
	if err != nil {
		return nil, err
	}
	for _, t := range todos {
		if !t.Open {
			continue
		}
		if strings.Contains(t.Text, JobsDir) {
			h.Jobs = append(h.Jobs, t.Text)
		} else {
			h.Todos = append(h.Todos, t.Text)
		}
	}
	if h.Changes, err = s.Changes(ChangeFilter{Host: name, Limit: 3}); err != nil {
		return nil, err
	}
	return h, nil
}

// ---- changes, todos, links, audits -------------------------

// ChangeFilter narrows Changes. Since is a YYYY-MM-DD date, Q a
// substring of headline, detail or who, case-insensitive.
type ChangeFilter struct {
	Host  string
	Since string
	Q     string
	Limit int
}

// Changes lists changelog entries, newest first.
func (s *Store) Changes(f ChangeFilter) ([]parse.Change, error) {
	q := `SELECT host, date, time, who, headline, detail, rollback, flags FROM changes WHERE 1=1`
	var args []any
	if f.Host != "" {
		q += ` AND host = ?`
		args = append(args, f.Host)
	}
	if f.Since != "" {
		q += ` AND date >= ?`
		args = append(args, f.Since)
	}
	if f.Q != "" {
		q += ` AND (instr(lower(headline), ?) > 0 OR instr(lower(detail), ?) > 0 OR instr(lower(who), ?) > 0 OR instr(lower(host), ?) > 0)`
		l := strings.ToLower(f.Q)
		args = append(args, l, l, l, l)
	}
	q += ` ORDER BY date DESC, time DESC, host, id`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	out := []parse.Change{}
	err := s.each(q, func(sc scanner) error {
		var c parse.Change
		var detail string
		if err := sc.Scan(&c.Host, &c.Date, &c.Time, &c.Who, &c.Headline, &detail, &c.Rollback, &c.Flags); err != nil {
			return err
		}
		c.Detail = []string{}
		if detail != "" {
			c.Detail = strings.Split(detail, "\x1f")
		}
		out = append(out, c)
		return nil
	}, args...)
	return out, err
}

// TodoRow is an open or done item with its host.
type TodoRow struct {
	Host string `json:"host"`
	Text string `json:"text"`
	Open bool   `json:"open"`
}

// Todos lists items; host "" means every host.
func (s *Store) Todos(host string) ([]TodoRow, error) {
	q := `SELECT host, text, open FROM todos`
	var args []any
	if host != "" {
		q += ` WHERE host = ?`
		args = append(args, host)
	}
	q += ` ORDER BY host, rowid`
	out := []TodoRow{}
	err := s.each(q, func(sc scanner) error {
		var t TodoRow
		var open int
		if err := sc.Scan(&t.Host, &t.Text, &open); err != nil {
			return err
		}
		t.Open = open == 1
		out = append(out, t)
		return nil
	}, args...)
	return out, err
}

// Links lists the network links.
func (s *Store) Links() ([]parse.Link, error) {
	out := []parse.Link{}
	err := s.each(`SELECT src, dst, label FROM links ORDER BY rowid`, func(sc scanner) error {
		var l parse.Link
		if err := sc.Scan(&l.Src, &l.Dst, &l.Label); err != nil {
			return err
		}
		out = append(out, l)
		return nil
	})
	return out, err
}

// Snapshots lists audit snapshot stamps, newest first.
func (s *Store) Snapshots() ([]string, error) {
	out := []string{}
	err := s.each(`SELECT DISTINCT snapshot FROM audits ORDER BY snapshot DESC`, func(sc scanner) error {
		var x string
		if err := sc.Scan(&x); err != nil {
			return err
		}
		out = append(out, x)
		return nil
	})
	return out, err
}

func (s *Store) newestSnapshot() (string, error) {
	snaps, err := s.Snapshots()
	if err != nil || len(snaps) == 0 {
		return "", err
	}
	return snaps[0], nil
}

// SnapshotDate turns a stamp 2026-10-28-0900 into "2026-10-28 09:00".
func SnapshotDate(stamp string) string {
	if len(stamp) != 15 {
		return stamp
	}
	return stamp[:10] + " " + stamp[11:13] + ":" + stamp[13:]
}

// AuditValues lists the values of one snapshot; host "" for all.
func (s *Store) AuditValues(snapshot, host string) ([]parse.AuditValue, error) {
	q := `SELECT host, key, value FROM audits WHERE snapshot = ?`
	args := []any{snapshot}
	if host != "" {
		q += ` AND host = ?`
		args = append(args, host)
	}
	q += ` ORDER BY host, key`
	out := []parse.AuditValue{}
	err := s.each(q, func(sc scanner) error {
		var v parse.AuditValue
		if err := sc.Scan(&v.Host, &v.Key, &v.Value); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	}, args...)
	return out, err
}

// AuditRow is one value with its snapshot.
type AuditRow struct {
	Snapshot string `json:"snapshot"`
	Date     string `json:"date"`
	Host     string `json:"host"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

// Audits lists every value of every snapshot; host "" for all.
func (s *Store) Audits(host string) ([]AuditRow, error) {
	q := `SELECT snapshot, host, key, value FROM audits`
	var args []any
	if host != "" {
		q += ` WHERE host = ?`
		args = append(args, host)
	}
	q += ` ORDER BY snapshot DESC, host, key`
	out := []AuditRow{}
	err := s.each(q, func(sc scanner) error {
		var r AuditRow
		if err := sc.Scan(&r.Snapshot, &r.Host, &r.Key, &r.Value); err != nil {
			return err
		}
		r.Date = SnapshotDate(r.Snapshot)
		out = append(out, r)
		return nil
	}, args...)
	return out, err
}

// ---- sessions ----------------------------------------------

// Session is an open jolitz session of one person on one host.
type Session struct {
	ID          string    `json:"id"`
	User        string    `json:"user"`
	Host        string    `json:"host"`
	Workstation string    `json:"workstation"`
	StartedAt   time.Time `json:"started_at"`
	LastBeat    time.Time `json:"last_beat"`
	AgeSeconds  int64     `json:"age_seconds"`
	Stale       bool      `json:"stale"`
}

// StartSession opens a session and returns its id.
func (s *Store) StartSession(user, host, workstation string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	now := s.now()
	_, err := s.db.Exec(`INSERT INTO sessions(id, user, host, workstation, started_at, last_beat) VALUES(?,?,?,?,?,?)`,
		id, user, host, workstation, now, now)
	return id, err
}

// Session returns one session, open or ended.
func (s *Store) Session(id string) (*Session, bool, error) {
	var se Session
	var started, beat string
	var ended sql.NullString
	err := s.db.QueryRow(`SELECT id, user, host, workstation, started_at, last_beat, ended_at FROM sessions WHERE id = ?`, id).
		Scan(&se.ID, &se.User, &se.Host, &se.Workstation, &started, &beat, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	se.StartedAt = parseTime(started)
	se.LastBeat = parseTime(beat)
	return &se, ended.Valid, nil
}

// Heartbeat marks a session alive; ErrNotFound when it is unknown
// or ended.
func (s *Store) Heartbeat(id string) error {
	if err := s.closeStale(); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE sessions SET last_beat = ? WHERE id = ? AND ended_at IS NULL`, s.now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// EndSession closes a session.
func (s *Store) EndSession(id string) error {
	res, err := s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ? AND ended_at IS NULL`, s.now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// closeStale ends sessions without a heartbeat for Closed.
func (s *Store) closeStale() error {
	limit := s.Now().UTC().Add(-Closed).Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE sessions SET ended_at = last_beat WHERE ended_at IS NULL AND last_beat <= ?`, limit)
	return err
}

// OpenSessions lists open sessions, oldest first, after closing
// the ones silent for Closed.
func (s *Store) OpenSessions() ([]Session, error) {
	if err := s.closeStale(); err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	out := []Session{}
	err := s.each(`SELECT id, user, host, workstation, started_at, last_beat FROM sessions WHERE ended_at IS NULL ORDER BY started_at`,
		func(sc scanner) error {
			var se Session
			var started, beat string
			if err := sc.Scan(&se.ID, &se.User, &se.Host, &se.Workstation, &started, &beat); err != nil {
				return err
			}
			se.StartedAt = parseTime(started)
			se.LastBeat = parseTime(beat)
			se.AgeSeconds = int64(now.Sub(se.StartedAt).Seconds())
			se.Stale = now.Sub(se.LastBeat) >= Stale
			out = append(out, se)
			return nil
		})
	return out, err
}

// ---- claims ------------------------------------------------

// Claim says who works on a host and why.
type Claim struct {
	Host  string    `json:"host"`
	User  string    `json:"user"`
	Note  string    `json:"note"`
	Since time.Time `json:"since"`
}

// Claims lists every claim, by host.
func (s *Store) Claims() ([]Claim, error) {
	out := []Claim{}
	err := s.each(`SELECT host, user, note, since FROM claims ORDER BY host`, func(sc scanner) error {
		var c Claim
		var since string
		if err := sc.Scan(&c.Host, &c.User, &c.Note, &since); err != nil {
			return err
		}
		c.Since = parseTime(since)
		out = append(out, c)
		return nil
	})
	return out, err
}

// Claim returns the claim on a host, or ErrNotFound.
func (s *Store) Claim(host string) (*Claim, error) {
	var c Claim
	var since string
	err := s.db.QueryRow(`SELECT host, user, note, since FROM claims WHERE host = ?`, host).Scan(&c.Host, &c.User, &c.Note, &since)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Since = parseTime(since)
	return &c, nil
}

// SetClaim claims a host for user. A host another person holds
// is left alone and ErrConflict returned, unless takeover is set
// (an admin); the caller decides who may do that. Renewing an
// own claim keeps its since and updates the note.
func (s *Store) SetClaim(host, user, note string, takeover bool) (*Claim, error) {
	if !parse.ValidHost(host) {
		return nil, fmt.Errorf("%w: host %q", ErrInvalid, host)
	}
	now := s.now()
	q := `INSERT INTO claims(host, user, note, since) VALUES(?,?,?,?)
		ON CONFLICT(host) DO UPDATE SET note = excluded.note WHERE claims.user = excluded.user`
	if takeover {
		q = `INSERT INTO claims(host, user, note, since) VALUES(?,?,?,?)
		ON CONFLICT(host) DO UPDATE SET user = excluded.user, note = excluded.note, since = excluded.since`
	}
	res, err := s.db.Exec(q, host, user, note, now)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	return s.Claim(host)
}

// DeleteClaim releases a host.
func (s *Store) DeleteClaim(host string) error {
	res, err := s.db.Exec(`DELETE FROM claims WHERE host = ?`, host)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- guard -------------------------------------------------

// AddGuard records one guard denial: when, who, why. Never the
// command.
func (s *Store) AddGuard(user string, at time.Time, reason string) error {
	_, err := s.db.Exec(`INSERT INTO guard(user, at, reason) VALUES(?,?,?)`, user, at.UTC().Format(time.RFC3339), reason)
	return err
}

// GuardCount is the number of denials for one reason.
type GuardCount struct {
	Reason string    `json:"reason"`
	Count  int       `json:"count"`
	Last   time.Time `json:"last"`
}

// GuardCounts groups the denials since a time by reason, most
// frequent first.
func (s *Store) GuardCounts(since time.Time) ([]GuardCount, error) {
	out := []GuardCount{}
	err := s.each(`SELECT reason, count(*), max(at) FROM guard WHERE at >= ? GROUP BY reason ORDER BY count(*) DESC, reason`,
		func(sc scanner) error {
			var g GuardCount
			var last string
			if err := sc.Scan(&g.Reason, &g.Count, &last); err != nil {
				return err
			}
			g.Last = parseTime(last)
			out = append(out, g)
			return nil
		}, since.UTC().Format(time.RFC3339))
	return out, err
}

// Tags lists every tag in the fleet, sorted.
func (s *Store) Tags() ([]string, error) {
	seen := map[string]bool{}
	err := s.each(`SELECT tags FROM hosts`, func(sc scanner) error {
		var t string
		if err := sc.Scan(&t); err != nil {
			return err
		}
		for _, x := range strings.Fields(t) {
			seen[x] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// ---- retention ---------------------------------------------

// PruneSessions deletes sessions that ended before t. Open ones
// stay; the heartbeat rule closes them first.
func (s *Store) PruneSessions(t time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE ended_at IS NOT NULL AND ended_at < ?`, t.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneGuard deletes guard denials recorded before t.
func (s *Store) PruneGuard(t time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM guard WHERE at < ?`, t.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- eol cache ---------------------------------------------

// EOLEntry is the cached answer of endoflife.date for one
// product: the raw JSON and the day it was fetched.
type EOLEntry struct {
	Product string
	Fetched string
	Body    []byte
}

// PutEOL stores or replaces the cached answer for a product.
func (s *Store) PutEOL(product, fetched string, body []byte) error {
	_, err := s.db.Exec(`INSERT INTO eol(product, fetched, body) VALUES(?,?,?)
		ON CONFLICT(product) DO UPDATE SET fetched = excluded.fetched, body = excluded.body`, product, fetched, body)
	return err
}

// EOL lists the cache, by product.
func (s *Store) EOL() ([]EOLEntry, error) {
	out := []EOLEntry{}
	err := s.each(`SELECT product, fetched, body FROM eol ORDER BY product`, func(sc scanner) error {
		var e EOLEntry
		if err := sc.Scan(&e.Product, &e.Fetched, &e.Body); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// ---- jobs --------------------------------------------------

// JobRun is the last run of one scheduled job.
type JobRun struct {
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	OK     bool      `json:"ok"`
	Result string    `json:"result"`
}

// SetJobRun records the last run of a job.
func (s *Store) SetJobRun(name string, at time.Time, ok bool, result string) error {
	okInt := 0
	if ok {
		okInt = 1
	}
	_, err := s.db.Exec(`INSERT INTO jobs(name, last_run, ok, result) VALUES(?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET last_run = excluded.last_run, ok = excluded.ok, result = excluded.result`,
		name, at.UTC().Format(time.RFC3339), okInt, result)
	return err
}

// JobRun returns the last run of a job; nil when it never ran.
func (s *Store) JobRun(name string) (*JobRun, error) {
	var r JobRun
	var at string
	var ok int
	err := s.db.QueryRow(`SELECT name, last_run, ok, result FROM jobs WHERE name = ?`, name).Scan(&r.Name, &at, &ok, &r.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.At = parseTime(at)
	r.OK = ok == 1
	return &r, nil
}

// JobRuns lists the last run of every job, by name.
func (s *Store) JobRuns() ([]JobRun, error) {
	out := []JobRun{}
	err := s.each(`SELECT name, last_run, ok, result FROM jobs ORDER BY name`, func(sc scanner) error {
		var r JobRun
		var at string
		var ok int
		if err := sc.Scan(&r.Name, &at, &ok, &r.Result); err != nil {
			return err
		}
		r.At = parseTime(at)
		r.OK = ok == 1
		out = append(out, r)
		return nil
	})
	return out, err
}

// ---- metrics -----------------------------------------------

// CountFiles is the number of stored files.
func (s *Store) CountFiles() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&n)
	return n, err
}

// LastPushByUser returns, per user, the time of their newest
// file write.
func (s *Store) LastPushByUser() (map[string]time.Time, error) {
	out := map[string]time.Time{}
	err := s.each(`SELECT updated_by, max(updated_at) FROM files GROUP BY updated_by`, func(sc scanner) error {
		var u, at string
		if err := sc.Scan(&u, &at); err != nil {
			return err
		}
		out[u] = parseTime(at)
		return nil
	})
	return out, err
}
