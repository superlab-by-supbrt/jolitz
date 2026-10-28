package store

import "testing"

func TestRebind(t *testing.T) {
	for _, c := range []struct{ in, sqlite, pg string }{
		{`SELECT a FROM t WHERE b = ?`, `SELECT a FROM t WHERE b = ?`, `SELECT a FROM t WHERE b = $1`},
		{`VALUES(?,?,?)`, `VALUES(?,?,?)`, `VALUES($1,$2,$3)`},
		// A ? inside a string literal is text, not a parameter.
		{`SELECT '?' || x FROM t WHERE y = ?`, `SELECT '?' || x FROM t WHERE y = ?`, `SELECT '?' || x FROM t WHERE y = $1`},
		{`SELECT 'it''s ?' WHERE z = ?`, `SELECT 'it''s ?' WHERE z = ?`, `SELECT 'it''s ?' WHERE z = $1`},
		{`WHERE instr(lower(h), ?) > 0`, `WHERE instr(lower(h), ?) > 0`, `WHERE strpos(lower(h), $1) > 0`},
	} {
		if got := rebind(false, c.in); got != c.sqlite {
			t.Errorf("sqlite %q = %q", c.in, got)
		}
		if got := rebind(true, c.in); got != c.pg {
			t.Errorf("postgres %q = %q, want %q", c.in, got, c.pg)
		}
	}
}
