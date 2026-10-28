package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The queries that differ most between SQLite and PostgreSQL:
// upserts, the quoted "user" column, ordering by id and by
// text, instr/strpos, empty bodies. Every assertion holds on
// both backends.

func TestSchemaVersion(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		st := b.new(t, time.Now())
		v, err := st.Version()
		if err != nil || v != SchemaVersion() || v < 2 {
			t.Errorf("version %d (%v), want %d", v, err, SchemaVersion())
		}
		if st.Backend() != b.name {
			t.Errorf("backend %s", st.Backend())
		}
	})
}

func TestUsersBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		st := b.new(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
		tok, err := st.AddUser("alice", RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddUser("alice", RoleViewer); !errors.Is(err, ErrExists) {
			t.Errorf("duplicate: %v", err)
		}
		u, err := st.UserByToken(tok)
		if err != nil || u == nil || u.Name != "alice" || u.Role != RoleAdmin {
			t.Errorf("by token %+v %v", u, err)
		}
		if u, _ := st.UserByName("alice"); u == nil || u.Role != RoleAdmin {
			t.Errorf("by name %+v", u)
		}
		if u, err := st.UserByName("nobody"); u != nil || err != nil {
			t.Errorf("unknown by name %+v %v", u, err)
		}
		tok2, err := st.RotateToken("alice")
		if err != nil {
			t.Fatal(err)
		}
		if u, _ := st.UserByToken(tok); u != nil {
			t.Error("old token still valid")
		}
		if u, _ := st.UserByToken(tok2); u == nil {
			t.Error("new token not valid")
		}
		if _, err := st.StartSession("alice", "web1.example.com", "mbp"); err != nil {
			t.Fatal(err)
		}
		if err := st.RemoveUser("alice"); err != nil {
			t.Fatal(err)
		}
		if open, _ := st.OpenSessions(); len(open) != 0 {
			t.Errorf("sessions of a removed user stay open: %+v", open)
		}
	})
}

func TestDerivedRowsBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		st := b.new(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
		put := func(p, body string) {
			t.Helper()
			if _, err := st.PutFile(p, []byte(body), "alice", ""); err != nil {
				t.Fatalf("%s: %v", p, err)
			}
		}
		// Byte order, not a locale's: '-' (0x2d) before '.' (0x2e)
		// before digits before upper before lower case.
		put("servers/web1.example.com/memory.md", "# web1.example.com\n- IP: 203.0.113.10\n- OS: Debian 12\n- Tags: prod, web\n- Owner: alice\n")
		put("servers/web-1.example.com/memory.md", "# web-1\n")
		put("servers/Web1.example.com/memory.md", "# Web1\n")
		put("servers/web1.example.com/todo.md", "- [ ] zebra\n- [x] apple\n- [ ] mango\n")
		put("servers/web1.example.com/changelog.log", "[2026-10-01 10:00] [alice as root] Nginx reloaded\n[2026-10-02 10:00] [bob as root] certificate renewed\n")
		put("servers/web1", "web1.example.com\n")
		put("network.md", "- Link: web1.example.com -> db1.example.com (z)\n- Link: web1.example.com -> cache.example.com (a)\n")
		put("servers/web1.example.com/rules.md", "")

		files, err := st.ListFiles()
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		want := []string{"network.md", "servers/Web1.example.com/memory.md", "servers/web-1.example.com/memory.md",
			"servers/web1", "servers/web1.example.com/changelog.log", "servers/web1.example.com/memory.md",
			"servers/web1.example.com/rules.md", "servers/web1.example.com/todo.md"}
		if len(paths) != len(want) {
			t.Fatalf("paths %q", paths)
		}
		for i := range want {
			if paths[i] != want[i] {
				t.Errorf("path %d = %q, want %q", i, paths[i], want[i])
			}
		}
		body, info, err := st.GetFile("servers/web1.example.com/rules.md")
		if err != nil || body == nil || len(body) != 0 || info.Size != 0 {
			t.Errorf("empty file: %q %+v %v", body, info, err)
		}

		hosts, err := st.Hosts()
		if err != nil || len(hosts) != 3 || hosts[0].Name != "Web1.example.com" || hosts[2].Name != "web1.example.com" {
			t.Fatalf("hosts %+v %v", hosts, err)
		}
		h := hosts[2]
		if h.IP != "203.0.113.10" || h.Owner != "alice" || len(h.Tags) != 2 || h.Todos != 2 || !h.Rules ||
			h.LastChange != "2026-10-02" || len(h.Aliases) != 1 || h.Aliases[0] != "web1" {
			t.Errorf("host %+v", h)
		}
		todos, _ := st.Todos("web1.example.com")
		if len(todos) != 3 || todos[0].Text != "zebra" || todos[1].Text != "apple" || todos[1].Open || todos[2].Text != "mango" {
			t.Errorf("todos in file order: %+v", todos)
		}
		links, _ := st.Links()
		if len(links) != 2 || links[0].Label != "z" || links[1].Label != "a" {
			t.Errorf("links in file order: %+v", links)
		}
		for q, n := range map[string]int{"nginx": 1, "CERTIFICATE": 1, "bob": 1, "web1.example": 2, "nothing": 0, "50%": 0, "'": 0} {
			got, err := st.Changes(ChangeFilter{Q: q})
			if err != nil || len(got) != n {
				t.Errorf("q=%q: %d changes (%v), want %d", q, len(got), err, n)
			}
		}
		if got, _ := st.Changes(ChangeFilter{Since: "2026-10-02", Limit: 5}); len(got) != 1 || got[0].Who != "bob as root" {
			t.Errorf("since: %+v", got)
		}

		// Rewriting a to-do list replaces the rows, order kept.
		put("servers/web1.example.com/todo.md", "- [ ] b\n- [ ] a\n")
		if todos, _ := st.Todos(""); len(todos) != 2 || todos[0].Text != "b" {
			t.Errorf("rewritten todos %+v", todos)
		}
		// Deleting every file of a host removes it.
		for _, p := range []string{"memory.md", "todo.md", "changelog.log", "rules.md"} {
			if err := st.DeleteFile("servers/web1.example.com/" + p); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := st.Host("web1.example.com"); !errors.Is(err, ErrNotFound) {
			t.Errorf("host after delete: %v", err)
		}
	})
}

func TestClaimsBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		c, err := st.SetClaim("db1.example.com", "bob", "migration", false)
		if err != nil || c.User != "bob" || !c.Since.Equal(now) {
			t.Fatalf("claim %+v %v", c, err)
		}
		st.Now = func() time.Time { return now.Add(time.Hour) }
		if c, err := st.SetClaim("db1.example.com", "bob", "until 20:00", false); err != nil || c.Note != "until 20:00" || !c.Since.Equal(now) {
			t.Errorf("renew %+v %v", c, err)
		}
		if _, err := st.SetClaim("db1.example.com", "alice", "mine", false); !errors.Is(err, ErrConflict) {
			t.Errorf("foreign claim: %v", err)
		}
		if c, err := st.SetClaim("db1.example.com", "alice", "taken", true); err != nil || c.User != "alice" {
			t.Errorf("takeover %+v %v", c, err)
		}
		if err := st.DeleteClaim("db1.example.com"); err != nil {
			t.Fatal(err)
		}
		if err := st.DeleteClaim("db1.example.com"); !errors.Is(err, ErrNotFound) {
			t.Errorf("second release: %v", err)
		}
	})
}

func TestSessionsBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		id, err := st.StartSession("alice", "web1.example.com", "mbp")
		if err != nil {
			t.Fatal(err)
		}
		st.Now = func() time.Time { return now.Add(11 * time.Minute) }
		open, _ := st.OpenSessions()
		if len(open) != 1 || !open[0].Stale || open[0].User != "alice" {
			t.Errorf("stale session %+v", open)
		}
		if err := st.Heartbeat(id); err != nil {
			t.Fatal(err)
		}
		st.Now = func() time.Time { return now.Add(11*time.Minute + 61*time.Minute) }
		if err := st.Heartbeat(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("heartbeat after close: %v", err)
		}
		if _, ended, err := st.Session(id); err != nil || !ended {
			t.Errorf("closed session: ended=%v %v", ended, err)
		}
	})
}

func TestWebSessionsBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		st := b.new(t, now)
		tok, err := st.AddUser("alice", RoleOperator)
		if err != nil {
			t.Fatal(err)
		}
		sec, err := st.StartWebSession("alice", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if u, err := st.UserByWebSession(sec); err != nil || u == nil || u.Name != "alice" || u.Role != RoleOperator {
			t.Errorf("web session %+v %v", u, err)
		}
		// A session secret is no API token, and a token no session.
		if u, _ := st.UserByToken(sec); u != nil {
			t.Error("session secret accepted as token")
		}
		if u, _ := st.UserByWebSession(tok); u != nil {
			t.Error("token accepted as session")
		}
		st.Now = func() time.Time { return now.Add(time.Hour) }
		if u, _ := st.UserByWebSession(sec); u != nil {
			t.Error("expired session accepted")
		}
		st.Now = func() time.Time { return now }
		sec2, _ := st.StartWebSession("alice", time.Hour)
		if err := st.EndWebSession(sec2); err != nil {
			t.Fatal(err)
		}
		if u, _ := st.UserByWebSession(sec2); u != nil {
			t.Error("ended session accepted")
		}
		sec3, _ := st.StartWebSession("alice", time.Hour)
		if err := st.RemoveUser("alice"); err != nil {
			t.Fatal(err)
		}
		if u, _ := st.UserByWebSession(sec3); u != nil {
			t.Error("session of a removed user accepted")
		}
	})
}

// Writers of one path queue up: every edit sees the one before
// it, every version number is used once.
func TestConcurrentUpdatesBothBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		st := b.new(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
		p := "servers/web1.example.com/todo.md"
		const n = 12
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			go func(i int) {
				_, err := st.UpdateFile(p, "alice", func(old []byte) ([]byte, error) {
					return append(old, fmt.Sprintf("- [ ] item %d\n", i)...), nil
				})
				errs <- err
			}(i)
		}
		for i := 0; i < n; i++ {
			if err := <-errs; err != nil {
				t.Errorf("update: %v", err)
			}
		}
		body, info, err := st.GetFile(p)
		if err != nil || info.Version != n || strings.Count(string(body), "\n") != n {
			t.Errorf("after %d concurrent edits: version %d, %d lines, %v", n, info.Version, strings.Count(string(body), "\n"), err)
		}
	})
}
