package store

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fillHub gives a SQLite hub what a team's hub holds: files with
// several versions, a deleted file with history, an alias, an
// audit, users, claims.
func fillHub(t *testing.T) (*Store, map[string]string) {
	t.Helper()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st := newStore(t, now.Add(-48*time.Hour))
	tokens := map[string]string{}
	for _, u := range []struct{ name, role string }{{"alice", RoleAdmin}, {"bob", RoleOperator}, {"carol", RoleViewer}} {
		tok, err := st.AddUser(u.name, u.role)
		if err != nil {
			t.Fatal(err)
		}
		tokens[u.name] = tok
	}
	if err := st.BindOIDC("alice", "https://id.example.com", "alice-sub"); err != nil {
		t.Fatal(err)
	}
	must := func(_ *FileInfo, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.PutFile("servers/web1.example.com/memory.md", []byte("# web1.example.com\n- IP: 203.0.113.10\n- OS: Debian 12\n- Owner: alice\n"), "alice", ""))
	must(st.PutFile("servers/web1.example.com/todo.md", []byte("- [ ] one\n"), "alice", ""))
	st.Now = func() time.Time { return now.Add(-24 * time.Hour) }
	must(st.UpdateFile("servers/web1.example.com/todo.md", "bob", func(old []byte) ([]byte, error) {
		return append(old, "- [x] two\n"...), nil
	}))
	must(st.PutFile("servers/web1.example.com/changelog.log", []byte("[2026-10-27 10:00] [bob as root] nginx reloaded\n  Rollback: none\n"), "bob", ""))
	must(st.PutFile("servers/web1", []byte("web1.example.com\n"), "alice", ""))
	must(st.PutFile("network.md", []byte("- Link: web1.example.com -> db1.example.com (pg)\n"), "alice", ""))
	must(st.PutFile("audits/2026-10-28-0900.json", []byte(`{"date":"2026-10-28 09:00","hosts":{"web1.example.com":{"sshd.PermitRootLogin":"no"}}}`), "alice", ""))
	must(st.PutFile("servers/old.example.com/memory.md", []byte("# old\n"), "alice", ""))
	must(st.PutFile("servers/old.example.com/memory.md", []byte("# old, v2\n"), "alice", ""))
	if err := st.DeleteFile("servers/old.example.com/memory.md"); err != nil {
		t.Fatal(err)
	}
	must(st.PutFile("servers/web1.example.com/rules.md", []byte{}, "alice", ""))
	st.Now = func() time.Time { return now }
	if _, err := st.SetClaim("web1.example.com", "bob", "kernel update", false); err != nil {
		t.Fatal(err)
	}
	return st, tokens
}

type hubState struct {
	Files   []FileInfo
	History map[string][]FileInfo
	Bodies  map[string]string
	Users   []User
	Claims  []Claim
	Hosts   []Host
	Changes int
	Links   int
	Audits  int
}

func snapshot(t *testing.T, st *Store, paths []string) hubState {
	t.Helper()
	var s hubState
	var err error
	if s.Files, err = st.ListFiles(); err != nil {
		t.Fatal(err)
	}
	s.History = map[string][]FileInfo{}
	s.Bodies = map[string]string{}
	for _, p := range paths {
		h, err := st.FileHistory(p)
		if err != nil {
			t.Fatal(err)
		}
		s.History[p] = h
		for _, v := range h {
			b, _, err := st.GetFileVersion(p, v.Version)
			if err != nil {
				t.Fatal(err)
			}
			s.Bodies[p+"@"+v.SHA256] = string(b)
		}
	}
	if s.Users, err = st.ListUsers(); err != nil {
		t.Fatal(err)
	}
	if s.Claims, err = st.Claims(); err != nil {
		t.Fatal(err)
	}
	if s.Hosts, err = st.Hosts(); err != nil {
		t.Fatal(err)
	}
	for i := range s.Hosts {
		s.Hosts[i].UpdatedAt = time.Time{} // derived anew on import
	}
	ch, _ := st.Changes(ChangeFilter{})
	s.Changes = len(ch)
	l, _ := st.Links()
	s.Links = len(l)
	a, _ := st.Audits("")
	s.Audits = len(a)
	return s
}

var hubPaths = []string{
	"servers/web1.example.com/memory.md", "servers/web1.example.com/todo.md",
	"servers/web1.example.com/changelog.log", "servers/web1", "network.md",
	"audits/2026-10-28-0900.json", "servers/old.example.com/memory.md",
	"servers/web1.example.com/rules.md",
}

func tarEntries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		out[h.Name] = body
	}
}

func TestExportImport(t *testing.T) {
	src, tokens := fillHub(t)
	var buf bytes.Buffer
	if err := src.Export(&buf, "test"); err != nil {
		t.Fatal(err)
	}
	ents := tarEntries(t, buf.Bytes())
	for _, name := range []string{"manifest.json", "users.json", "claims.json", "history.json",
		"memory/servers/web1.example.com/todo.md", "memory/network.md"} {
		if _, ok := ents[name]; !ok {
			t.Errorf("tar lacks %s", name)
		}
	}
	if _, ok := ents["memory/servers/old.example.com/memory.md"]; ok {
		t.Error("a deleted file is in memory/")
	}
	if string(ents["memory/servers/web1.example.com/todo.md"]) != "- [ ] one\n- [x] two\n" {
		t.Errorf("current todo.md %q", ents["memory/servers/web1.example.com/todo.md"])
	}
	var hist []map[string]any
	if err := json.Unmarshal(ents["history.json"], &hist); err != nil || len(hist) != 10 {
		t.Errorf("history.json: %d entries, %v", len(hist), err)
	}
	if !strings.Contains(string(ents["users.json"]), `"token_sha256"`) {
		t.Error("users.json without token hashes")
	}
	var users []map[string]any
	if err := json.Unmarshal(ents["users.json"], &users); err != nil || len(users) != 3 ||
		users[0]["oidc_issuer"] != "https://id.example.com" || users[0]["oidc_subject"] != "alice-sub" {
		t.Errorf("users.json binding of alice: %+v %v", users, err)
	}
	// Unbound users carry no binding fields, as format 1 always
	// had them; bob's entry is such an old one.
	if _, has := users[1]["oidc_issuer"]; has {
		t.Errorf("unbound bob has oidc fields: %+v", users[1])
	}
	want := snapshot(t, src, hubPaths)

	eachBackend(t, func(t *testing.T, b backend) {
		dst := b.new(t, time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC))
		n, err := dst.Import(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if n.Files != 7 || n.Versions != 10 || n.Users != 3 || n.Claims != 1 {
			t.Errorf("import counts %+v", n)
		}
		got := snapshot(t, dst, hubPaths)
		if !reflect.DeepEqual(got.Files, want.Files) {
			t.Errorf("manifest\n got %+v\nwant %+v", got.Files, want.Files)
		}
		if !reflect.DeepEqual(got.History, want.History) {
			t.Errorf("history\n got %+v\nwant %+v", got.History, want.History)
		}
		if !reflect.DeepEqual(got.Bodies, want.Bodies) {
			t.Errorf("bodies differ")
		}
		if !reflect.DeepEqual(got.Users, want.Users) || !reflect.DeepEqual(got.Claims, want.Claims) {
			t.Errorf("users/claims\n got %+v %+v\nwant %+v %+v", got.Users, got.Claims, want.Users, want.Claims)
		}
		if !reflect.DeepEqual(got.Hosts, want.Hosts) || got.Changes != want.Changes || got.Links != want.Links || got.Audits != want.Audits {
			t.Errorf("derived\n got %+v\nwant %+v", got, want)
		}
		if i, sub, ok, err := dst.OIDCBinding("alice"); !ok || err != nil || i != "https://id.example.com" || sub != "alice-sub" {
			t.Errorf("binding of alice after import: %q %q %v %v", i, sub, ok, err)
		}
		if _, _, ok, _ := dst.OIDCBinding("bob"); ok {
			t.Error("bob bound after import")
		}
		for name, tok := range tokens {
			if u, _ := dst.UserByToken(tok); u == nil || u.Name != name {
				t.Errorf("token of %s not accepted after import", name)
			}
		}
		// The history continues: the next write is the next number.
		f, err := dst.PutFile("servers/old.example.com/memory.md", []byte("# back\n"), "alice", "-")
		if err != nil || f.Version != 3 {
			t.Errorf("write after import: %+v %v", f, err)
		}
		// A second export carries the same history.
		var again bytes.Buffer
		if err := dst.Export(&again, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := dst.Import(bytes.NewReader(buf.Bytes())); !errors.Is(err, ErrNotEmpty) {
			t.Errorf("import into a non-empty database: %v", err)
		}
	})
}

func TestImportRejects(t *testing.T) {
	src, _ := fillHub(t)
	var buf bytes.Buffer
	if err := src.Export(&buf, "test"); err != nil {
		t.Fatal(err)
	}
	ents := tarEntries(t, buf.Bytes())
	rebuild := func(edit func(map[string][]byte)) []byte {
		e := map[string][]byte{}
		for k, v := range ents {
			e[k] = v
		}
		edit(e)
		var out bytes.Buffer
		tw := tar.NewWriter(&out)
		for k, v := range e {
			tw.WriteHeader(&tar.Header{Name: k, Mode: 0o600, Size: int64(len(v)), Typeflag: tar.TypeReg})
			tw.Write(v)
		}
		tw.Close()
		return out.Bytes()
	}
	for name, edit := range map[string]func(map[string][]byte){
		"tampered body": func(e map[string][]byte) {
			for k := range e {
				if strings.HasPrefix(k, "bodies/") {
					e[k] = []byte("changed")
					return
				}
			}
		},
		"missing body": func(e map[string][]byte) {
			for k := range e {
				if strings.HasPrefix(k, "bodies/") {
					delete(e, k)
					return
				}
			}
		},
		"no manifest":    func(e map[string][]byte) { delete(e, "manifest.json") },
		"unknown format": func(e map[string][]byte) { e["manifest.json"] = []byte(`{"format":99}`) },
		"bad path": func(e map[string][]byte) {
			e["history.json"] = []byte(strings.Replace(string(e["history.json"]), "network.md", "../etc/passwd", 1))
		},
		"bad role": func(e map[string][]byte) {
			e["users.json"] = []byte(strings.Replace(string(e["users.json"]), `"viewer"`, `"root"`, 1))
		},
		"unknown entry": func(e map[string][]byte) { e["evil.sh"] = []byte("x") },
		"half a binding": func(e map[string][]byte) {
			e["users.json"] = []byte(strings.Replace(string(e["users.json"]), `"oidc_subject": "alice-sub"`, `"oidc_subject": ""`, 1))
		},
		"one pair twice": func(e map[string][]byte) {
			e["users.json"] = []byte(strings.Replace(string(e["users.json"]), `"name": "bob",`,
				`"name": "bob", "oidc_issuer": "https://id.example.com", "oidc_subject": "alice-sub",`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dst := newStore(t, time.Now())
			if _, err := dst.Import(bytes.NewReader(rebuild(edit))); err == nil {
				t.Fatal("import accepted")
			}
			if files, _ := dst.ListFiles(); len(files) != 0 {
				t.Errorf("a failed import left %d files", len(files))
			}
			if users, _ := dst.ListUsers(); len(users) != 0 {
				t.Errorf("a failed import left %d users", len(users))
			}
		})
	}
}
