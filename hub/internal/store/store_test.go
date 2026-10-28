package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPruneSessionsAndGuard(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		old := now.AddDate(0, 0, -100)
		st.Now = func() time.Time { return old }
		oldID, err := st.StartSession("alice", "web1.example.com", "mbp")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.EndSession(oldID); err != nil {
			t.Fatal(err)
		}
		// An old session that never ended is closed by the heartbeat
		// rule, not by retention: its ended_at is its last beat.
		if _, err := st.StartSession("alice", "web1.example.com", "mbp"); err != nil {
			t.Fatal(err)
		}
		if err := st.AddGuard("alice", old, "old reason"); err != nil {
			t.Fatal(err)
		}
		st.Now = func() time.Time { return now }
		newID, err := st.StartSession("bob", "db1.example.com", "pc")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.EndSession(newID); err != nil {
			t.Fatal(err)
		}
		if err := st.AddGuard("bob", now.Add(-time.Hour), "new reason"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.OpenSessions(); err != nil { // closes the stale one
			t.Fatal(err)
		}
		n, err := st.PruneSessions(now.AddDate(0, 0, -90))
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("pruned %d sessions, want 2", n)
		}
		if _, _, err := st.Session(oldID); err == nil {
			t.Error("old session survived")
		}
		if _, _, err := st.Session(newID); err != nil {
			t.Error("recent session pruned")
		}
		n, err = st.PruneGuard(now.AddDate(0, 0, -30))
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("pruned %d guard rows, want 1", n)
		}
		gs, err := st.GuardCounts(time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(gs) != 1 || gs[0].Reason != "new reason" {
			t.Errorf("guard rows after prune: %+v", gs)
		}
	})
}

func TestEOLCache(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		if err := st.PutEOL("debian", "2026-10-27", []byte(`{"result":{}}`)); err != nil {
			t.Fatal(err)
		}
		if err := st.PutEOL("debian", "2026-10-28", []byte(`{"result":{"releases":[]}}`)); err != nil {
			t.Fatal(err)
		}
		if err := st.PutEOL("ubuntu", "2026-10-28", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		rows, err := st.EOL()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].Product != "debian" || rows[0].Fetched != "2026-10-28" || string(rows[0].Body) != `{"result":{"releases":[]}}` {
			t.Errorf("eol rows %+v", rows)
		}
	})
}

func TestJobRuns(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		if r, err := st.JobRun("eol"); err != nil || r != nil {
			t.Fatalf("unknown job = %+v, %v", r, err)
		}
		if err := st.SetJobRun("eol", now.Add(-time.Hour), false, "debian: HTTP 503"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetJobRun("eol", now, true, "2 products"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetJobRun("retention", now, true, "0 sessions, 0 guard rows"); err != nil {
			t.Fatal(err)
		}
		r, err := st.JobRun("eol")
		if err != nil {
			t.Fatal(err)
		}
		if r == nil || !r.OK || r.Result != "2 products" || !r.At.Equal(now) {
			t.Errorf("job run %+v", r)
		}
		all, err := st.JobRuns()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 || all[0].Name != "eol" || all[1].Name != "retention" {
			t.Errorf("job runs %+v", all)
		}
	})
}

func TestLastPushByUser(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now.Add(-2*time.Hour))
		if _, err := st.PutFile("network.md", []byte("# Network\n"), "alice", ""); err != nil {
			t.Fatal(err)
		}
		st.Now = func() time.Time { return now }
		if _, err := st.PutFile("servers/db1.example.com/todo.md", []byte("- [ ] x\n"), "alice", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.PutFile("servers/web1.example.com/todo.md", []byte("- [ ] y\n"), "bob", ""); err != nil {
			t.Fatal(err)
		}
		last, err := st.LastPushByUser()
		if err != nil {
			t.Fatal(err)
		}
		if len(last) != 2 || !last["alice"].Equal(now) || !last["bob"].Equal(now) {
			t.Errorf("last push %+v", last)
		}
		n, err := st.CountFiles()
		if err != nil || n != 3 {
			t.Errorf("files = %d, %v", n, err)
		}
	})
}

func TestFileVersions(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		p := "servers/web1.example.com/todo.md"
		f1, err := st.PutFile(p, []byte("- [ ] one\n"), "alice", "")
		if err != nil || f1.Version != 1 {
			t.Fatalf("first put: %+v %v", f1, err)
		}
		st.Now = func() time.Time { return now.Add(time.Hour) }
		f2, err := st.PutFile(p, []byte("- [x] one\n"), "bob", f1.SHA256)
		if err != nil || f2.Version != 2 || f2.UpdatedBy != "bob" {
			t.Fatalf("second put: %+v %v", f2, err)
		}
		if _, err := st.PutFile(p, []byte("x"), "bob", f1.SHA256); !errors.Is(err, ErrMismatch) {
			t.Errorf("stale If-Match: %v", err)
		}
		body, cur, err := st.GetFile(p)
		if err != nil || cur.Version != 2 || string(body) != "- [x] one\n" {
			t.Errorf("current: %s %+v %v", body, cur, err)
		}
		old, info, err := st.GetFileVersion(p, 1)
		if err != nil || string(old) != "- [ ] one\n" || info.UpdatedBy != "alice" || info.SHA256 != f1.SHA256 {
			t.Errorf("version 1: %s %+v %v", old, info, err)
		}
		if _, _, err := st.GetFileVersion(p, 3); !errors.Is(err, ErrNotFound) {
			t.Errorf("version 3: %v", err)
		}
		hist, err := st.FileHistory(p)
		if err != nil || len(hist) != 2 || hist[0].Version != 2 || hist[1].Version != 1 || hist[1].UpdatedAt != now {
			t.Errorf("history %+v %v", hist, err)
		}
		files, _ := st.ListFiles()
		if len(files) != 1 || files[0].Version != 2 {
			t.Errorf("manifest %+v", files)
		}

		// UpdateFile edits in place and makes the next version.
		f3, err := st.UpdateFile(p, "carol", func(old []byte) ([]byte, error) {
			return append(old, []byte("- [ ] two\n")...), nil
		})
		if err != nil || f3.Version != 3 || f3.UpdatedBy != "carol" {
			t.Fatalf("update: %+v %v", f3, err)
		}
		todos, _ := st.Todos("web1.example.com")
		if len(todos) != 2 || !todos[1].Open {
			t.Errorf("derived todos %+v", todos)
		}
		// An error from the edit changes nothing.
		wantErr := errors.New("no")
		if _, err := st.UpdateFile(p, "carol", func([]byte) ([]byte, error) { return nil, wantErr }); !errors.Is(err, wantErr) {
			t.Errorf("edit error: %v", err)
		}
		// A missing file hands nil to the edit and is created.
		f, err := st.UpdateFile("network.md", "carol", func(old []byte) ([]byte, error) {
			if old != nil {
				t.Errorf("old = %q, want nil", old)
			}
			return []byte("# Network\n"), nil
		})
		if err != nil || f.Version != 1 {
			t.Errorf("create by update: %+v %v", f, err)
		}

		// Delete keeps the history; a new file continues the count.
		if err := st.DeleteFile(p); err != nil {
			t.Fatal(err)
		}
		hist, _ = st.FileHistory(p)
		if len(hist) != 3 {
			t.Errorf("history after delete %+v", hist)
		}
		f4, err := st.PutFile(p, []byte("- [ ] again\n"), "alice", "-")
		if err != nil || f4.Version != 4 {
			t.Errorf("put after delete: %+v %v", f4, err)
		}

		// Pruning drops old history, never a current version.
		st.Now = func() time.Time { return now.Add(48 * time.Hour) }
		n, err := st.PruneFileVersions(now.Add(24 * time.Hour))
		if err != nil || n != 3 {
			t.Errorf("pruned %d (%v), want 3", n, err)
		}
		hist, _ = st.FileHistory(p)
		if len(hist) != 1 || hist[0].Version != 4 {
			t.Errorf("history after prune %+v", hist)
		}
		if _, _, err := st.GetFile("network.md"); err != nil {
			t.Errorf("network.md pruned: %v", err)
		}

		// A deleted file keeps no history past the cutoff: its
		// content does not outlive the delete forever.
		if err := st.DeleteFile(p); err != nil {
			t.Fatal(err)
		}
		st.Now = func() time.Time { return now.Add(96 * time.Hour) }
		if _, err := st.PruneFileVersions(now.Add(72 * time.Hour)); err != nil {
			t.Fatal(err)
		}
		if hist, _ = st.FileHistory(p); len(hist) != 0 {
			t.Errorf("history of a deleted file after prune %+v, want none", hist)
		}
	})
}

func TestMigrateFromPhaseOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE files(path TEXT PRIMARY KEY, body BLOB NOT NULL, sha256 TEXT NOT NULL,
		size INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL);
		INSERT INTO files VALUES('network.md', X'2320', 'abc', 2, '2026-01-01T00:00:00Z', 'alice')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, info, err := st.GetFile("network.md")
	if err != nil || info.Version != 1 {
		t.Fatalf("migrated file %+v %v", info, err)
	}
	hist, err := st.FileHistory("network.md")
	if err != nil || len(hist) != 1 || hist[0].UpdatedBy != "alice" {
		t.Errorf("migrated history %+v %v", hist, err)
	}
	if _, err := st.PutFile("network.md", []byte("# Network\n"), "bob", "abc"); err != nil {
		t.Errorf("put after migration: %v", err)
	}
	// Opening twice runs the migration twice without harm.
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}

func TestHandover(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		if _, err := st.Handover("db1.example.com"); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown host: %v", err)
		}
		must := func(_ *FileInfo, err error) {
			if err != nil {
				t.Fatal(err)
			}
		}
		must(st.PutFile("servers/db1.example.com/memory.md", []byte("# db1.example.com\n"), "alice", ""))
		must(st.PutFile("servers/db1.example.com/todo.md", []byte("- [x] done\n- [ ] Upgrade PostgreSQL\n- [ ] dist-upgrade running in ~/.jolitz/jobs/20261028-1200-upgrade\n"), "alice", ""))
		must(st.PutFile("servers/db1.example.com/changelog.log", []byte("[2026-10-01 10:00] [alice as root] one\n  Rollback: undo one\n[2026-10-02 10:00] [alice as root] two\n[2026-10-03 10:00] [alice as root] three\n  Rollback: undo three\n[2026-10-04 10:00] [bob as root] four\n"), "alice", ""))
		if _, err := st.SetClaim("db1.example.com", "bob", "migration", false); err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartSession("bob", "db1.example.com", "pc"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartSession("alice", "web1.example.com", "mbp"); err != nil {
			t.Fatal(err)
		}
		h, err := st.Handover("db1.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if h.Claim == nil || h.Claim.User != "bob" || len(h.Sessions) != 1 || h.Sessions[0].User != "bob" {
			t.Errorf("claim/sessions %+v", h)
		}
		if len(h.Todos) != 1 || h.Todos[0] != "Upgrade PostgreSQL" || len(h.Jobs) != 1 || !strings.Contains(h.Jobs[0], JobsDir) {
			t.Errorf("todos/jobs %+v", h)
		}
		if len(h.Changes) != 3 || h.Changes[0].Headline != "four" || h.Changes[1].Rollback != "undo three" || h.Changes[2].Headline != "two" {
			t.Errorf("changes %+v", h.Changes)
		}
	})
}

// A phase-2 database predates schema_migrations: its tables are
// there, todos and links have no id. Opening it numbers it,
// keeps the rows in their order and adds web_sessions.
func TestMigrateFromPhaseTwo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ms[0].SQL); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO files VALUES('servers/web1.example.com/todo.md', X'2D205B205D20620A2D205B205D20610A', 'abc', 16, '2026-01-01T00:00:00Z', 'alice', 1);
		INSERT INTO file_versions VALUES('servers/web1.example.com/todo.md', 1, X'2D205B205D20620A2D205B205D20610A', 'abc', 16, '2026-01-01T00:00:00Z', 'alice');
		INSERT INTO todos VALUES('web1.example.com', 'b', 1);
		INSERT INTO todos VALUES('web1.example.com', 'a', 1);
		INSERT INTO links VALUES('x', 'y', 'one');
		INSERT INTO links VALUES('x', 'z', 'two')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, err := st.Version(); err != nil || v != SchemaVersion() {
		t.Errorf("version %d %v", v, err)
	}
	todos, err := st.Todos("")
	if err != nil || len(todos) != 2 || todos[0].Text != "b" || todos[1].Text != "a" {
		t.Errorf("todos %+v %v", todos, err)
	}
	links, err := st.Links()
	if err != nil || len(links) != 2 || links[1].Label != "two" {
		t.Errorf("links %+v %v", links, err)
	}
	hist, err := st.FileHistory("servers/web1.example.com/todo.md")
	if err != nil || len(hist) != 1 {
		t.Errorf("history %+v %v", hist, err)
	}
	if _, err := st.StartWebSession("alice", time.Hour); err != nil {
		t.Errorf("web_sessions: %v", err)
	}
}

// A database at schema 2 with users gets the OIDC binding
// columns, every user unbound, and the unique index on the pair.
func TestMigrateSchemaTwoToThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range ms[:2] {
		if _, err := db.Exec(m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES(?,?,'2026-10-01T00:00:00Z')`, m.Version, m.Name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(name, token_sha256, role, created_at) VALUES
		('alice', 'aa', 'admin', '2026-10-01T00:00:00Z'), ('bob', 'bb', 'viewer', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, err := st.Version(); err != nil || v != 3 || SchemaVersion() < 3 {
		t.Errorf("version %d %v", v, err)
	}
	users, err := st.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("users %+v %v", users, err)
	}
	for _, u := range users {
		if _, _, ok, err := st.OIDCBinding(u.Name); ok || err != nil {
			t.Errorf("%s bound after the migration: %v", u.Name, err)
		}
	}
	if err := st.BindOIDC("alice", "https://id.example.com", "s1"); err != nil {
		t.Fatal(err)
	}
	// The index holds even past the store's own checks.
	if _, err := st.db.Exec(`UPDATE users SET oidc_issuer = 'https://id.example.com', oidc_subject = 's1' WHERE name = 'bob'`); err == nil {
		t.Error("two users with one pair")
	}
	// Two unbound users are no conflict.
	if _, err := st.AddUser("carol", RoleViewer); err != nil {
		t.Errorf("third unbound user: %v", err)
	}
}
