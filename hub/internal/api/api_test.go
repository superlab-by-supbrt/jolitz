package api

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

type env struct {
	t      *testing.T
	st     *store.Store
	srv    *httptest.Server
	tokens map[string]string
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, tokens: map[string]string{}, now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	st.Now = func() time.Time { return e.now }
	for name, role := range map[string]string{"viewer": "viewer", "op": "operator", "op2": "operator", "admin": "admin"} {
		tok, err := st.AddUser(name, role)
		if err != nil {
			t.Fatal(err)
		}
		e.tokens[name] = tok
	}
	e.srv = httptest.NewServer(New(Config{Store: st, Version: "test"}))
	t.Cleanup(func() { e.srv.Close(); st.Close() })
	return e
}

// do sends a request as user (""= no token, "wrong" = bad token).
func (e *env) do(user, method, path string, body string, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+"/api/v1/"+path, strings.NewReader(body))
	switch user {
	case "":
	case "wrong":
		req.Header.Set("Authorization", "Bearer not-a-token")
	default:
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func (e *env) want(code int, res *http.Response, body, what string) {
	e.t.Helper()
	if res.StatusCode != code {
		e.t.Errorf("%s: got %d %s, want %d", what, res.StatusCode, body, code)
	}
}

const memoryMD = "# web1.example.com\n- IP: 203.0.113.10\n- OS: Debian 12 (Bookworm)\n- Tags: prod, web\n- Owner: alice\n- Services: nginx 1.26, postgresql 17\n- Last connected: 2026-10-28\n- Database password: in /etc/app/db.env (0600 root)\n"

func TestHealthAndAuthMatrix(t *testing.T) {
	e := newEnv(t)
	res, body := e.do("", "GET", "health", "")
	e.want(200, res, body, "health")
	if !strings.Contains(body, `"ok": true`) || !strings.Contains(body, `"version": "test"`) {
		t.Errorf("health body %s", body)
	}
	for _, c := range []struct {
		user, method, path string
		code               int
	}{
		{"", "GET", "files", 401},
		{"wrong", "GET", "files", 401},
		{"viewer", "GET", "files", 200},
		{"viewer", "PUT", "files/network.md", 403},
		{"viewer", "POST", "sessions", 403},
		{"viewer", "PUT", "claims/web1.example.com", 403},
		{"viewer", "POST", "guard", 403},
		{"op", "PUT", "files/network.md", 201},
		{"", "GET", "hosts", 401},
		{"wrong", "PUT", "files/network.md", 401},
		{"admin", "GET", "hosts", 200},
	} {
		res, body := e.do(c.user, c.method, c.path, "# Network\n")
		e.want(c.code, res, body, c.user+" "+c.method+" "+c.path)
		if c.code == 401 || c.code == 403 {
			if strings.Contains(body, "Network") || !strings.Contains(body, `"error"`) {
				t.Errorf("refusal leaked data or has no error: %s", body)
			}
		}
	}
	res, _ = e.do("", "GET", "files", "")
	if !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Error("401 without WWW-Authenticate")
	}
	// The login cookie works like the bearer token.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/whoami", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: e.tokens["viewer"]})
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != 200 {
		t.Errorf("cookie auth = %d", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestFilesRoundtrip(t *testing.T) {
	e := newEnv(t)
	body := "[2026-06-13 08:00] [alice as root] quoted \"value\" and back\\slash\n  Detail: bytes \x00\xff stay\r\n"
	res, out := e.do("op", "PUT", "files/servers/web1.example.com/changelog.log", body)
	e.want(201, res, out, "put new")
	var info store.FileInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil || info.SHA256 != store.SHA256([]byte(body)) || info.UpdatedBy != "op" || info.Size != int64(len(body)) {
		t.Errorf("put response %s (%v)", out, err)
	}
	res, got := e.do("viewer", "GET", "files/servers/web1.example.com/changelog.log", "")
	e.want(200, res, got, "get")
	if got != body {
		t.Errorf("roundtrip differs: %q", got)
	}
	if res.Header.Get("ETag") != `"`+info.SHA256+`"` {
		t.Errorf("ETag %q", res.Header.Get("ETag"))
	}
	res, out = e.do("viewer", "GET", "files", "")
	e.want(200, res, out, "manifest")
	var files []store.FileInfo
	if err := json.Unmarshal([]byte(out), &files); err != nil || len(files) != 1 || files[0].Path != "servers/web1.example.com/changelog.log" {
		t.Errorf("manifest %s", out)
	}

	// If-Match: matching sha updates, a stale one is 412 and
	// leaves the file alone.
	res, out = e.do("op", "PUT", "files/servers/web1.example.com/changelog.log", body+"[2026-06-14 08:00] x\n", "If-Match", info.SHA256)
	e.want(200, res, out, "put with matching If-Match")
	res, out = e.do("op", "PUT", "files/servers/web1.example.com/changelog.log", "other", "If-Match", `"`+info.SHA256+`"`)
	e.want(412, res, out, "put with stale If-Match")
	_, got = e.do("viewer", "GET", "files/servers/web1.example.com/changelog.log", "")
	if got != body+"[2026-06-14 08:00] x\n" {
		t.Errorf("412 changed the file: %q", got)
	}
	res, out = e.do("op", "PUT", "files/network.md", "new", "If-Match", "abc")
	e.want(412, res, out, "If-Match on a missing file")

	res, out = e.do("op", "DELETE", "files/servers/web1.example.com/changelog.log", "")
	e.want(200, res, out, "delete")
	res, out = e.do("viewer", "GET", "files/servers/web1.example.com/changelog.log", "")
	e.want(404, res, out, "get after delete")
	res, out = e.do("viewer", "GET", "hosts", "")
	e.want(200, res, out, "hosts after delete")
	if out != "[]\n" {
		t.Errorf("host survives its last file: %s", out)
	}
}

func TestLint422(t *testing.T) {
	e := newEnv(t)
	tok := "gh" + "p_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	body := memoryMD + "- Deploy token: " + tok + "\n"
	res, out := e.do("op", "PUT", "files/servers/web1.example.com/memory.md", body)
	e.want(422, res, out, "token in memory.md")
	var hit struct {
		Line   int
		Reason string
	}
	if err := json.Unmarshal([]byte(out), &hit); err != nil || hit.Line != 9 || hit.Reason == "" {
		t.Errorf("422 body %s", out)
	}
	if strings.Contains(out, tok) {
		t.Error("422 body echoes the secret")
	}
	res, out = e.do("viewer", "GET", "files", "")
	if out != "[]\n" {
		t.Errorf("a refused file was stored: %s", out)
	}
	res, out = e.do("viewer", "GET", "hosts", "")
	if out != "[]\n" {
		t.Errorf("a refused file derived a host: %s", out)
	}
	key := "-----BEGIN OPENSSH " + "PRIVATE KEY-----"
	res, out = e.do("op", "PUT", "files/servers/db1.example.com/changelog.log", "[2026-01-01 10:00] x\n  Detail: "+key+"\n")
	e.want(422, res, out, "private key header")
	res, out = e.do("op", "PUT", "files/network.md", "mysql://app:s3c"+"ret@db1/app\n")
	e.want(422, res, out, "password in a URL")
	// A location is not a value.
	res, out = e.do("op", "PUT", "files/servers/web1.example.com/memory.md", memoryMD)
	e.want(201, res, out, "password location passes")
}

func TestPaths400(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{
		"servers/a%3Cb/memory.md", "servers/a%22b/memory.md", "servers/a%2Fb/memory.md",
		"servers/web1/notes.txt", "servers/-bad/memory.md", "servers/.hidden/memory.md",
		"user.md", "blacklist.md", "audits/notes.json", "audits/2026-10-28.json",
		"servers/web1/memory.md/extra", "x/network.md",
	} {
		res, out := e.do("op", "PUT", "files/"+p, "x")
		if res.StatusCode != 400 && res.StatusCode != 404 && res.StatusCode != 301 {
			t.Errorf("PUT %s = %d %s, want 400", p, res.StatusCode, out)
		}
	}
	res, out := e.do("viewer", "GET", "hosts/a%3Cb", "")
	e.want(400, res, out, "host name with <")
	// The allowed set.
	for _, p := range []string{
		"servers/web1.example.com/memory.md", "servers/web1.example.com/changelog.log",
		"servers/web1.example.com/todo.md", "servers/web1.example.com/rules.md",
		"network.md", "audits/2026-10-28-0900.json",
	} {
		body := "# x\n"
		if strings.HasPrefix(p, "audits/") {
			body = `{"date":"2026-10-28 09:00","hosts":{}}`
		}
		res, out := e.do("op", "PUT", "files/"+p, body)
		e.want(201, res, out, "allowed "+p)
	}
	res, out = e.do("op", "PUT", "files/servers/www.example.com", "web1.example.com\n")
	e.want(201, res, out, "alias file")
	res, out = e.do("op", "PUT", "files/servers/www2.example.com", "two\nlines\n")
	e.want(400, res, out, "alias with two lines")
	res, out = e.do("op", "PUT", "files/audits/2026-10-28-1000.json", "{not json")
	e.want(400, res, out, "broken audit json")
	res, out = e.do("viewer", "GET", "hosts", "")
	var hosts []store.Host
	if err := json.Unmarshal([]byte(out), &hosts); err != nil || len(hosts) != 1 || hosts[0].Aliases[0] != "www.example.com" || !hosts[0].Rules {
		t.Errorf("hosts %s", out)
	}
}

func TestHostsParsed(t *testing.T) {
	e := newEnv(t)
	e.do("op", "PUT", "files/servers/web1.example.com/memory.md", memoryMD)
	e.do("op", "PUT", "files/servers/web1.example.com/changelog.log", "[2026-06-11 09:50] [alice as root]\napp deploys <script>alert(1)</script>\n  Flags: careful.\n[2026-06-12 08:00] [alice as root] reloaded\n")
	e.do("op", "PUT", "files/servers/db1.example.com/todo.md", "- [x] done\n- [ ] Upgrade to Debian 12\n")
	e.do("op", "PUT", "files/network.md", "- Link: web1.example.com -> db1.example.com (postgresql 5432)\n")
	e.do("op", "PUT", "files/audits/2026-10-28-0900.json", `{"date":"2026-10-28 09:00","hosts":{"web1.example.com":{"time.ntp":"chrony"}}}`)
	res, out := e.do("viewer", "GET", "hosts", "")
	e.want(200, res, out, "hosts")
	var hosts []store.Host
	if err := json.Unmarshal([]byte(out), &hosts); err != nil || len(hosts) != 2 {
		t.Fatalf("hosts %s", out)
	}
	db1, web1 := hosts[0], hosts[1]
	if db1.Name != "db1.example.com" || db1.Todos != 1 || db1.OS != "" {
		t.Errorf("db1 %+v", db1)
	}
	if web1.Owner != "alice" || web1.Tags[1] != "web" || web1.Services[1].Version != "17" || web1.LastChange != "2026-06-12" || web1.LastConnectedDays != 0 || web1.Audit["time.ntp"] != "chrony" || web1.Product != "debian" {
		t.Errorf("web1 %+v", web1)
	}
	res, out = e.do("viewer", "GET", "hosts/web1.example.com", "")
	e.want(200, res, out, "host")
	var d store.HostDetail
	if err := json.Unmarshal([]byte(out), &d); err != nil || len(d.Changes) != 2 || d.Changes[1].Flags != "careful." || d.MemoryMD != memoryMD || d.AuditDate != "2026-10-28 09:00" {
		t.Errorf("host detail %s", out)
	}
	if d.Changes[1].Headline != "app deploys <script>alert(1)</script>" {
		t.Errorf("headline altered: %q", d.Changes[1].Headline)
	}
	res, out = e.do("viewer", "GET", "hosts/nope.example.com", "")
	e.want(404, res, out, "unknown host")
	res, out = e.do("viewer", "GET", "changes?q=SCRIPT&limit=1", "")
	e.want(200, res, out, "changes search")
	if !strings.Contains(out, "alert(1)") || strings.Count(out, `"date"`) != 1 {
		t.Errorf("changes %s", out)
	}
	res, out = e.do("viewer", "GET", "changes?since=2026-06-12", "")
	if strings.Contains(out, "alert(1)") {
		t.Errorf("since filter %s", out)
	}
	res, out = e.do("viewer", "GET", "todos?host=db1.example.com", "")
	if !strings.Contains(out, "Upgrade to Debian 12") {
		t.Errorf("todos %s", out)
	}
	res, out = e.do("viewer", "GET", "links", "")
	if !strings.Contains(out, `"label": "postgresql 5432"`) {
		t.Errorf("links %s", out)
	}
	res, out = e.do("viewer", "GET", "audits?host=web1.example.com", "")
	if !strings.Contains(out, `"snapshot": "2026-10-28-0900"`) {
		t.Errorf("audits %s", out)
	}
}

func TestExportTar(t *testing.T) {
	e := newEnv(t)
	want := map[string]string{
		"servers/web1.example.com/memory.md": memoryMD,
		"servers/www.example.com":            "web1.example.com\n",
		"network.md":                         "# Network\n",
		"audits/2026-10-28-0900.json":        `{"date":"2026-10-28 09:00","hosts":{}}`,
	}
	for p, b := range want {
		e.do("op", "PUT", "files/"+p, b)
	}
	res, out := e.do("viewer", "GET", "export", "")
	e.want(200, res, out, "export")
	if res.Header.Get("Content-Type") != "application/x-tar" {
		t.Errorf("content type %q", res.Header.Get("Content-Type"))
	}
	tr := tar.NewReader(bytes.NewReader([]byte(out)))
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		got[h.Name] = string(b)
	}
	if len(got) != len(want) {
		t.Errorf("tar lists %v", got)
	}
	for p, b := range want {
		if got[p] != b {
			t.Errorf("tar %s = %q", p, got[p])
		}
	}
}

func TestSessions(t *testing.T) {
	e := newEnv(t)
	e.do("op", "PUT", "files/servers/web1.example.com/memory.md", memoryMD)
	res, out := e.do("op", "POST", "sessions", `{"host":"web1.example.com","workstation":"alices-mbp"}`)
	e.want(201, res, out, "start")
	var id struct{ ID string }
	json.Unmarshal([]byte(out), &id)
	res, out = e.do("op2", "POST", "sessions", `{"host":"web1.example.com","workstation":"bobs-pc"}`)
	e.want(201, res, out, "second start")
	var id2 struct{ ID string }
	json.Unmarshal([]byte(out), &id2)
	res, out = e.do("op", "POST", "sessions", `{"host":"a<b"}`)
	e.want(400, res, out, "bad host")

	res, out = e.do("viewer", "GET", "sessions", "")
	e.want(200, res, out, "list")
	var ss []store.Session
	json.Unmarshal([]byte(out), &ss)
	if len(ss) != 2 || ss[0].User != "op" || ss[1].User != "op2" || ss[0].Stale {
		t.Errorf("sessions %s", out)
	}
	res, out = e.do("viewer", "GET", "hosts", "")
	if !strings.Contains(out, `"workstation": "alices-mbp"`) || !strings.Contains(out, `"workstation": "bobs-pc"`) {
		t.Errorf("both names on the host: %s", out)
	}

	// 10 minutes of silence: stale, still listed.
	e.now = e.now.Add(10 * time.Minute)
	res, out = e.do("viewer", "GET", "sessions", "")
	json.Unmarshal([]byte(out), &ss)
	if len(ss) != 2 || !ss[0].Stale || ss[0].AgeSeconds != 600 {
		t.Errorf("stale sessions %s", out)
	}
	// A heartbeat revives the first one.
	res, out = e.do("op", "POST", "sessions/"+id.ID+"/heartbeat", "")
	e.want(200, res, out, "heartbeat")
	res, out = e.do("op2", "POST", "sessions/"+id.ID+"/heartbeat", "")
	e.want(403, res, out, "heartbeat on someone else's session")
	// 60 minutes without a beat closes the second.
	e.now = e.now.Add(50 * time.Minute)
	res, out = e.do("viewer", "GET", "sessions", "")
	json.Unmarshal([]byte(out), &ss)
	if len(ss) != 1 || ss[0].ID != id.ID || !ss[0].Stale {
		t.Errorf("after 60 min %s", out)
	}
	res, out = e.do("op2", "POST", "sessions/"+id2.ID+"/heartbeat", "")
	e.want(404, res, out, "heartbeat on a closed session")

	res, out = e.do("op2", "DELETE", "sessions/"+id.ID, "")
	e.want(403, res, out, "end someone else's session")
	res, out = e.do("admin", "DELETE", "sessions/"+id.ID, "")
	e.want(200, res, out, "admin ends it")
	res, out = e.do("viewer", "GET", "sessions", "")
	if out != "[]\n" {
		t.Errorf("sessions left %s", out)
	}
	res, out = e.do("op", "DELETE", "sessions/unknown", "")
	e.want(404, res, out, "end unknown")
}

func TestClaims(t *testing.T) {
	e := newEnv(t)
	res, out := e.do("op", "PUT", "claims/db1.example.com", `{"note":"migration until 20:00"}`)
	e.want(200, res, out, "claim")
	res, out = e.do("op", "PUT", "claims/a%3Cb", `{}`)
	e.want(400, res, out, "claim bad host")
	res, out = e.do("viewer", "GET", "claims", "")
	if !strings.Contains(out, `"user": "op"`) || !strings.Contains(out, "migration until 20:00") {
		t.Errorf("claims %s", out)
	}
	res, out = e.do("op2", "DELETE", "claims/db1.example.com", "")
	e.want(403, res, out, "release someone else's claim")
	// Another operator cannot take the claim over; the claim is
	// unchanged afterwards. An admin can.
	res, out = e.do("op2", "PUT", "claims/db1.example.com", `{"note":"mine now"}`)
	e.want(409, res, out, "takeover by another operator")
	if !strings.Contains(out, `"claimed_by": "op"`) || !strings.Contains(out, `"since"`) {
		t.Errorf("409 body %s", out)
	}
	_, out = e.do("viewer", "GET", "claims", "")
	if !strings.Contains(out, `"user": "op"`) || !strings.Contains(out, "migration until 20:00") || strings.Contains(out, "mine now") {
		t.Errorf("claim changed by a 409: %s", out)
	}
	res, out = e.do("op", "PUT", "claims/db1.example.com", `{"note":"migration until 21:00"}`)
	e.want(200, res, out, "own renewal")
	_, out = e.do("viewer", "GET", "claims", "")
	if !strings.Contains(out, "21:00") {
		t.Errorf("renewal lost: %s", out)
	}
	res, out = e.do("admin", "PUT", "claims/db1.example.com", `{"note":"admin takes over"}`)
	e.want(200, res, out, "admin takeover")
	if !strings.Contains(out, `"user": "admin"`) {
		t.Errorf("admin takeover body %s", out)
	}
	res, out = e.do("op", "DELETE", "claims/db1.example.com", "")
	e.want(403, res, out, "release the admin.s claim")
	res, out = e.do("admin", "PUT", "claims/db1.example.com", `{"note":"back"}`)
	e.want(200, res, out, "admin renews")
	res, out = e.do("admin", "DELETE", "claims/db1.example.com", "")
	e.want(200, res, out, "admin releases own")
	res, out = e.do("op", "PUT", "claims/db1.example.com", `{"note":"migration until 20:00"}`)
	e.want(200, res, out, "op claims again")
	res, out = e.do("op", "DELETE", "claims/db1.example.com", "")
	e.want(200, res, out, "release own")
	res, out = e.do("op", "DELETE", "claims/db1.example.com", "")
	e.want(404, res, out, "release twice")
	e.do("op", "PUT", "claims/db1.example.com", `{"note":"x"}`)
	res, out = e.do("admin", "DELETE", "claims/db1.example.com", "")
	e.want(200, res, out, "admin releases")
	// A claim on a host that has files shows on the host.
	e.do("op", "PUT", "files/servers/db1.example.com/memory.md", "# db1.example.com\n")
	e.do("op2", "PUT", "claims/db1.example.com", `{"note":"patching"}`)
	res, out = e.do("viewer", "GET", "hosts/db1.example.com", "")
	if !strings.Contains(out, `"note": "patching"`) {
		t.Errorf("host claim %s", out)
	}
}

func TestGuard(t *testing.T) {
	e := newEnv(t)
	res, out := e.do("op", "POST", "guard", `{"time":"2026-10-28T10:00:00Z","reason":"powering off a machine","command":"halt"}`)
	e.want(400, res, out, "command key")
	res, out = e.do("op", "POST", "guard", `{"time":"2026-10-28T10:00:00Z","reason":"powering off a machine"}`)
	e.want(201, res, out, "denial")
	res, out = e.do("op", "POST", "guard", `{"reason":"powering off a machine"}`)
	e.want(201, res, out, "denial without time")
	res, out = e.do("op", "POST", "guard", `{"time":"2026-01-01T00:00:00Z","reason":"old"}`)
	e.want(201, res, out, "old denial")
	res, out = e.do("op", "POST", "guard", `{"reason":""}`)
	e.want(400, res, out, "empty reason")
	res, out = e.do("op", "POST", "guard", `{"time":"yesterday","reason":"x"}`)
	e.want(400, res, out, "bad time")
	res, out = e.do("viewer", "GET", "guard", "")
	e.want(200, res, out, "counts")
	var gs []store.GuardCount
	json.Unmarshal([]byte(out), &gs)
	if len(gs) != 1 || gs[0].Count != 2 || gs[0].Reason != "powering off a machine" {
		t.Errorf("guard counts %s", out)
	}
	res, out = e.do("viewer", "GET", "guard?since=2025-01-01", "")
	json.Unmarshal([]byte(out), &gs)
	if len(gs) != 2 {
		t.Errorf("guard since %s", out)
	}
	if strings.Contains(out, "halt") {
		t.Error("command text stored")
	}
}

func TestEndpointsRegistered(t *testing.T) {
	e := newEnv(t)
	for _, ep := range Endpoints {
		p := strings.NewReplacer("{path}", "network.md", "{name}", "x.example.com", "{id}", "abc", "{host}", "x.example.com").Replace(ep.Path)
		res, out := e.do("admin", ep.Method, p, `{"reason":"x","note":"n"}`)
		if res.StatusCode == 405 || res.StatusCode == 500 || (res.StatusCode == 404 && strings.Contains(out, "no such endpoint")) {
			t.Errorf("%s %s = %d %s", ep.Method, ep.Path, res.StatusCode, out)
		}
	}
	res, out := e.do("admin", "GET", "nothing", "")
	e.want(404, res, out, "unknown endpoint")
}
