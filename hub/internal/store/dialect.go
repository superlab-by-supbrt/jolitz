package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"strconv"
	"strings"
)

// The store writes every query once, in the dialect both
// backends share: ? placeholders, ON CONFLICT upserts, "user"
// quoted, times as RFC 3339 text. rebind turns a query into
// PostgreSQL's spelling: $1, $2, … for the placeholders and
// strpos for SQLite's instr (same argument order). A ? inside a
// '…' literal stays as it is.
func rebind(pg bool, q string) string {
	if !pg {
		return q
	}
	q = strings.ReplaceAll(q, "instr(", "strpos(")
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	inStr := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			inStr = !inStr
			b.WriteByte(c)
		case c == '?' && !inStr:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// db is *sql.DB with every query rebound for its backend.
type db struct {
	*sql.DB
	pg bool
}

func (d *db) Exec(q string, args ...any) (sql.Result, error) {
	return d.DB.Exec(rebind(d.pg, q), args...)
}

func (d *db) Query(q string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(rebind(d.pg, q), args...)
}

func (d *db) QueryRow(q string, args ...any) *sql.Row {
	return d.DB.QueryRow(rebind(d.pg, q), args...)
}

func (d *db) Begin() (*txn, error) {
	t, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &txn{Tx: t, pg: d.pg}, nil
}

// txn is *sql.Tx with every query rebound for its backend.
type txn struct {
	*sql.Tx
	pg bool
}

func (t *txn) Exec(q string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(rebind(t.pg, q), args...)
}

func (t *txn) Query(q string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(rebind(t.pg, q), args...)
}

func (t *txn) QueryRow(q string, args ...any) *sql.Row {
	return t.Tx.QueryRow(rebind(t.pg, q), args...)
}

// lockPath makes the writers of one path queue up until the
// transaction ends, so that each reads what the one before it
// wrote. On PostgreSQL that is a transaction-scoped advisory lock
// on a key derived from the path; on SQLite every transaction
// begins IMMEDIATE (see Open) and already holds the one write
// lock.
func (t *txn) lockPath(path string) error {
	if !t.pg {
		return nil
	}
	sum := sha256.Sum256([]byte("jolitz-hub file " + path))
	_, err := t.Exec(`SELECT pg_advisory_xact_lock(?)`, int64(binary.BigEndian.Uint64(sum[:8])))
	return err
}
