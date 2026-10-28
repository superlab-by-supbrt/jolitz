package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Now = func() time.Time { return now }
	t.Cleanup(func() { st.Close() })
	return st
}

func TestPruneSessionsAndGuard(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st := newStore(t, now)
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
}

func TestEOLCache(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st := newStore(t, now)
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
}

func TestJobRuns(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st := newStore(t, now)
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
}

func TestLastPushByUser(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st := newStore(t, now.Add(-2*time.Hour))
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
}
