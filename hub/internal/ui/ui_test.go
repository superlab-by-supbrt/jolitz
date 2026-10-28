package ui

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// The fixture of tests/memory-tools-test.sh, pushed through the
// store as jolitz-sync would.
var fixture = map[string]string{
	"servers/web1.example.com/memory.md": `# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- SSH port: 2222
- Owner: alice
- Services: nginx 1.26, postgresql 17
- Last connected: 2026-10-28
- Database password: in /etc/app/db.env (0600 root)
`,
	"servers/web1.example.com/changelog.log": `[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green <script>alert(1)</script>
— because deploys interrupted visitors
  Detail: templated app@.service (blue :4003,
  green :4005).
  Rollback: re-enable app.service, restore vhost
  backup from /var/backups/jolitz/.
  Verify: 135 probes, all 200.
  Flags: migrations must now stay
  backward-compatible.
[2026-06-12 08:00] [alice as root] nginx reloaded (auto, policy)
[2026-06-13 08:00] [alice as root] quoted "value" and back\slash
`,
	"servers/web1.example.com/rules.md": "## Replace: Firewall\nnftables only.\n",
	"servers/www.example.com":           "web1.example.com\n",
	"servers/db1.example.com/memory.md": `# db1.example.com
- IP: 203.0.113.20
- OS: Debian 11 (Bullseye)
- Distro family: debian
- Tags: prod, Database
- Last connected: 2025-01-01
`,
	"servers/db1.example.com/todo.md":       "# To-do\n- [x] Back up pg_hba.conf\n- [ ] Upgrade to Debian 12\n",
	"servers/db1.example.com/changelog.log": "[2025-01-01 10:00] [bob as root] PostgreSQL 15 installed\n[2025-01-02] forgot the timestamp format\n",
	"servers/mac.local/memory.md":           "# mac.local\n- OS: macOS 15.1 Sequoia\n- Mode: local\n- Last connected: yesterday\n",
	"network.md":                            "# Network\nAll servers sit in one VLAN; db1 is reached over the private IP.\n- Link: web1.example.com -> db1.example.com (postgresql 5432)\n",
	"audits/2026-10-01-0900.json": `{"date":"2026-10-01 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"}}}
`,
	"audits/2026-10-28-0900.json": `{"date":"2026-10-28 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"yes","time.ntp":"unknown(needs-root)"}},
 "skipped":{"mac.local":"macOS not covered"}}
`,
}

type env struct {
	t      *testing.T
	st     *store.Store
	srv    *httptest.Server
	tokens map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	e := &env{t: t, st: st, tokens: map[string]string{}}
	for name, role := range map[string]string{"viewer": "viewer", "alice": "operator", "bob": "operator", "root": "admin"} {
		e.tokens[name], err = st.AddUser(name, role)
		if err != nil {
			t.Fatal(err)
		}
	}
	for p, b := range fixture {
		if _, err := st.PutFile(p, []byte(b), "alice", ""); err != nil {
			t.Fatalf("fixture %s: %v", p, err)
		}
	}
	if _, err := st.StartSession("alice", "web1.example.com", "alices-mbp"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSession("bob", "web1.example.com", "bobs-pc"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetClaim("db1.example.com", "bob", "migration until 20:00", false); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGuard("alice", now.Add(-time.Hour), "powering off a machine"); err != nil {
		t.Fatal(err)
	}
	e.srv = httptest.NewServer(New(Config{Store: st, Version: "1.2.3-test", BaseURL: "https://hub.example.com"}))
	t.Cleanup(func() { e.srv.Close(); st.Close() })
	return e
}

func (e *env) get(user, path string) (*http.Response, string) {
	e.t.Helper()
	return e.req(user, "GET", path, nil)
}

func (e *env) req(user, method, path string, form url.Values) (*http.Response, string) {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if user != "" {
		req.AddCookie(&http.Cookie{Name: api.CookieName, Value: e.tokens[user]})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

var routes = []string{"/", "/hosts/web1.example.com", "/hosts/db1.example.com", "/hosts/mac.local", "/timeline", "/timeline?days=365", "/activity", "/activity?q=nginx", "/presence", "/audit", "/guard", "/api"}

func TestEveryRouteRenders(t *testing.T) {
	e := newEnv(t)
	for _, p := range routes {
		res, body := e.get("viewer", p)
		if res.StatusCode != 200 {
			t.Errorf("%s = %d", p, res.StatusCode)
		}
		if !strings.Contains(body, "jolitz hub") || !strings.Contains(body, "1.2.3-test") || !strings.Contains(body, `href="/api"`) {
			t.Errorf("%s lacks top bar or footer", p)
		}
		if !strings.Contains(body, "<b>3</b> hosts") || !strings.Contains(body, "<b>2</b> online") {
			t.Errorf("%s top bar counts wrong", p)
		}
		if strings.Contains(body, "<script>alert(1)") {
			t.Errorf("%s carries raw markup from memory", p)
		}
		if !strings.Contains(body, "font-family") && !strings.Contains(body, "monospace") {
			t.Errorf("%s has no inline CSS", p)
		}
		if strings.Contains(body, "src=\"http") || strings.Contains(body, "href=\"http") {
			if !strings.Contains(p, "/api") {
				t.Errorf("%s references an external asset", p)
			}
		}
	}
}

func TestEscapedHeadline(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/hosts/web1.example.com", "/timeline?days=365", "/activity"} {
		_, body := e.get("viewer", p)
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s does not show the escaped headline", p)
		}
		if strings.Contains(body, "<script>alert(1)") {
			t.Errorf("%s shows raw markup", p)
		}
	}
	_, body := e.get("viewer", "/hosts/web1.example.com")
	if !strings.Contains(body, "quoted &#34;value&#34; and back\\slash") && !strings.Contains(body, `quoted "value" and back\slash`) {
		t.Errorf("quote/backslash entry missing")
	}
	if !strings.Contains(body, "migrations must now stay backward-compatible") || !strings.Contains(body, "Rollback: re-enable app.service") {
		t.Errorf("host page lacks constraints or rollback detail")
	}
	if !strings.Contains(body, "nftables only.") || !strings.Contains(body, "- Database password: in /etc/app/db.env (0600 root)") {
		t.Errorf("host page lacks rules.md or memory.md")
	}
	if !strings.Contains(body, "also known as") || !strings.Contains(body, "www.example.com") {
		t.Errorf("host page lacks alias")
	}
	if !strings.Contains(body, "alices-mbp") || !strings.Contains(body, "bobs-pc") {
		t.Errorf("host page lacks both sessions")
	}
}

func TestFleetPage(t *testing.T) {
	e := newEnv(t)
	_, body := e.get("viewer", "/")
	for _, want := range []string{
		`<a href="/hosts/db1.example.com">db1.example.com</a>`, "also www.example.com", "own rules.md",
		"<td>alice</td>", `data-filter="prod"`, `data-tags="prod web"`, "ssh port 2222",
		"Upgrade to Debian 12", "alices-mbp", "bobs-pc", "migration until 20:00",
		`<span class="pill bad">2025-01-01 (665 days)</span>`, "2026-10-28 (today)", `id="q"`, `data-poll="30"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fleet lacks %q", want)
		}
	}
	if strings.Contains(body, "Back up pg_hba.conf") {
		t.Error("done to-do listed")
	}
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)
	res, _ := e.get("", "/")
	if res.StatusCode != 303 || res.Header.Get("Location") != "/login" {
		t.Errorf("/ without cookie = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	for _, p := range routes[1:] {
		if res, _ := e.get("", p); res.StatusCode != 303 {
			t.Errorf("%s without cookie = %d", p, res.StatusCode)
		}
	}
	res, body := e.get("", "/login")
	if res.StatusCode != 200 || !strings.Contains(body, `name="token"`) {
		t.Errorf("/login = %d", res.StatusCode)
	}
	if strings.Contains(body, "web1.example.com") {
		t.Error("login page leaks data")
	}
	res, body = e.req("", "POST", "/login", url.Values{"token": {"wrong"}})
	if res.StatusCode != 401 || !strings.Contains(body, "not known") {
		t.Errorf("wrong token = %d", res.StatusCode)
	}
	res, _ = e.req("", "POST", "/login", url.Values{"token": {e.tokens["alice"]}})
	if res.StatusCode != 303 || res.Header.Get("Location") != "/" {
		t.Errorf("login = %d", res.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == api.CookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Value != e.tokens["alice"] || cookie.Secure {
		t.Errorf("cookie %+v", cookie)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/login", strings.NewReader(url.Values{"token": {e.tokens["alice"]}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r2, _ := client.Do(req)
	r2.Body.Close()
	for _, c := range r2.Cookies() {
		if c.Name == api.CookieName && !c.Secure {
			t.Error("cookie behind TLS proxy is not Secure")
		}
	}
	res, _ = e.req("alice", "POST", "/logout", url.Values{})
	if res.StatusCode != 303 {
		t.Errorf("logout = %d", res.StatusCode)
	}
	res, _ = e.get("", "/nothing")
	if res.StatusCode != 404 {
		t.Errorf("unknown page = %d", res.StatusCode)
	}
	res, _ = e.get("viewer", "/hosts/a%3Cb")
	if res.StatusCode != 400 {
		t.Errorf("bad host name = %d", res.StatusCode)
	}
	res, _ = e.get("viewer", "/hosts/nope.example.com")
	if res.StatusCode != 404 {
		t.Errorf("unknown host = %d", res.StatusCode)
	}
}

func TestClaimButtons(t *testing.T) {
	e := newEnv(t)
	_, body := e.get("viewer", "/hosts/web1.example.com")
	if strings.Contains(body, `action="/hosts/web1.example.com/claim"`) {
		t.Error("viewer sees a claim button")
	}
	_, body = e.get("alice", "/hosts/web1.example.com")
	if !strings.Contains(body, `action="/hosts/web1.example.com/claim"`) {
		t.Error("operator sees no claim button")
	}
	res, _ := e.req("viewer", "POST", "/hosts/web1.example.com/claim", url.Values{"note": {"x"}})
	if res.StatusCode != 403 {
		t.Errorf("viewer claim = %d", res.StatusCode)
	}
	res, _ = e.req("alice", "POST", "/hosts/web1.example.com/claim", url.Values{"note": {"patching <b>now</b>"}})
	if res.StatusCode != 303 {
		t.Errorf("claim = %d", res.StatusCode)
	}
	_, body = e.get("bob", "/hosts/web1.example.com")
	if !strings.Contains(body, "patching &lt;b&gt;now&lt;/b&gt;") || strings.Contains(body, "<b>now</b>") {
		t.Error("claim note not escaped")
	}
	if strings.Contains(body, `action="/hosts/web1.example.com/release"`) {
		t.Error("bob can release alice's claim")
	}
	res, _ = e.req("bob", "POST", "/hosts/web1.example.com/release", url.Values{})
	if res.StatusCode != 403 {
		t.Errorf("bob release = %d", res.StatusCode)
	}
	res, _ = e.req("bob", "POST", "/hosts/web1.example.com/claim", url.Values{"note": {"mine"}})
	if res.StatusCode != 409 {
		t.Errorf("bob takeover = %d", res.StatusCode)
	}
	_, body = e.get("bob", "/hosts/web1.example.com")
	if !strings.Contains(body, "patching") || strings.Contains(body, ">mine<") {
		t.Error("claim changed by a refused takeover")
	}
	_, body = e.get("root", "/hosts/web1.example.com")
	if !strings.Contains(body, `action="/hosts/web1.example.com/release"`) {
		t.Error("admin sees no release button")
	}
	res, _ = e.req("alice", "POST", "/hosts/web1.example.com/release", url.Values{})
	if res.StatusCode != 303 {
		t.Errorf("own release = %d", res.StatusCode)
	}
	_, body = e.get("alice", "/hosts/web1.example.com")
	if !strings.Contains(body, "not claimed") {
		t.Error("claim survives release")
	}
}

func TestPagesContent(t *testing.T) {
	e := newEnv(t)
	_, body := e.get("viewer", "/presence")
	for _, w := range []string{"alices-mbp", "bobs-pc", `class="pill ok">live`, "migration until 20:00", `data-poll="30"`} {
		if !strings.Contains(body, w) {
			t.Errorf("presence lacks %q", w)
		}
	}
	_, body = e.get("viewer", "/audit")
	for _, w := range []string{"snapshot 2026-10-28 09:00", "compared with 2026-10-01 09:00", "<b>1</b> settings differ",
		"<code>sshd.PermitRootLogin</code> <code>no</code> → <code>yes</code>", `class="drift"`} {
		if !strings.Contains(body, w) {
			t.Errorf("audit lacks %q", w)
		}
	}
	_, body = e.get("viewer", "/guard")
	if !strings.Contains(body, "powering off a machine") || !strings.Contains(body, "<b>1</b> denials") {
		t.Errorf("guard page: %s", body)
	}
	_, body = e.get("viewer", "/timeline?days=365")
	if !strings.Contains(body, "<svg") || !strings.Contains(body, "<rect") || !strings.Contains(body, "<circle") {
		t.Error("timeline lacks svg marks")
	}
	_, body = e.get("viewer", "/timeline")
	if strings.Contains(body, "<circle") {
		t.Error("30-day timeline shows old changes")
	}
	_, body = e.get("viewer", "/activity?q=quoted")
	if !strings.Contains(body, "back\\slash") || strings.Contains(body, "nginx reloaded") {
		t.Error("activity search wrong")
	}
	_, body = e.get("viewer", "/api")
	for _, ep := range api.Endpoints {
		if !strings.Contains(body, "/api/v1/"+ep.Path+"</code>") {
			t.Errorf("/api lacks %s %s", ep.Method, ep.Path)
		}
	}
	if !strings.Contains(body, "curl -sS") || !strings.Contains(body, "https://hub.example.com") {
		t.Error("/api lacks curl examples with the base url")
	}
	res, body := e.get("viewer", "/export")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/x-tar" {
		t.Fatalf("export = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	tr := tar.NewReader(bytes.NewReader([]byte(body)))
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := fixture[h.Name]; !ok {
			t.Errorf("unexpected tar entry %s", h.Name)
		}
		n++
	}
	if n != len(fixture) {
		t.Errorf("tar has %d entries, want %d", n, len(fixture))
	}
}

// fileOf reads a file from the store for the edit tests.
func (e *env) fileOf(p string) (string, *store.FileInfo) {
	e.t.Helper()
	b, info, err := e.st.GetFile(p)
	if err != nil {
		e.t.Fatalf("%s: %v", p, err)
	}
	return string(b), info
}

func TestHandoverBlock(t *testing.T) {
	e := newEnv(t)
	_, body := e.get("viewer", "/hosts/web1.example.com")
	i := strings.Index(body, `<section class="handover">`)
	j := strings.Index(body, "<h2>facts</h2>")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("handover block missing or below facts (%d, %d)", i, j)
	}
	block := body[i:j]
	for _, want := range []string{"Rollback: re-enable app.service", "2026-06-13 08:00", "alices-mbp", "bobs-pc", "not claimed", "nothing open"} {
		if !strings.Contains(block, want) {
			t.Errorf("handover block lacks %q", want)
		}
	}
	if strings.Contains(block, "<script>alert(1)") || !strings.Contains(block, "&lt;script&gt;alert(1)") {
		t.Error("handover block does not escape the headline")
	}
	_, body = e.get("viewer", "/hosts/db1.example.com")
	block = body[strings.Index(body, `<section class="handover">`):strings.Index(body, "<h2>facts</h2>")]
	for _, want := range []string{"migration until 20:00", "Upgrade to Debian 12", "PostgreSQL 15 installed", "no way back recorded"} {
		if !strings.Contains(block, want) {
			t.Errorf("db1 handover block lacks %q", want)
		}
	}
	if strings.Contains(block, "Back up pg_hba.conf") {
		t.Error("handover lists a done to-do")
	}
	// A running job is listed apart from the to-dos.
	e.st.UpdateFile("servers/db1.example.com/todo.md", "alice", func(old []byte) ([]byte, error) {
		return append(old, []byte("- [ ] dist-upgrade running in ~/.jolitz/jobs/20261028-1200-upgrade\n")...), nil
	})
	_, body = e.get("viewer", "/hosts/db1.example.com")
	block = body[strings.Index(body, `<section class="handover">`):strings.Index(body, "<h2>facts</h2>")]
	if !strings.Contains(block, "running jobs") || !strings.Contains(block, "~/.jolitz/jobs/20261028-1200-upgrade") {
		t.Error("handover lacks the running job")
	}
}

func TestTodoEdits(t *testing.T) {
	e := newEnv(t)
	p := "/hosts/db1.example.com/todos"
	res, _ := e.req("viewer", "POST", p, url.Values{"action": {"toggle"}, "text": {"Upgrade to Debian 12"}, "done": {"1"}})
	if res.StatusCode != 403 {
		t.Errorf("viewer toggle = %d", res.StatusCode)
	}
	_, body := e.get("viewer", "/hosts/db1.example.com")
	if strings.Contains(body, `action="/hosts/db1.example.com/todos"`) {
		t.Error("viewer sees to-do forms")
	}
	_, body = e.get("alice", "/hosts/db1.example.com")
	if !strings.Contains(body, `action="/hosts/db1.example.com/todos"`) || !strings.Contains(body, `name="done" value="1"`) {
		t.Error("operator sees no to-do forms")
	}
	res, _ = e.req("alice", "POST", p, url.Values{"action": {"toggle"}, "text": {"Upgrade to Debian 12"}, "done": {"1"}})
	if res.StatusCode != 303 || res.Header.Get("Location") != "/hosts/db1.example.com" {
		t.Fatalf("toggle = %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	got, info := e.fileOf("servers/db1.example.com/todo.md")
	if got != "# To-do\n- [x] Back up pg_hba.conf\n- [x] Upgrade to Debian 12\n" || info.Version != 2 || info.UpdatedBy != "alice" {
		t.Errorf("after check: %q %+v", got, info)
	}
	res, _ = e.req("alice", "POST", p, url.Values{"action": {"toggle"}, "text": {"Back up pg_hba.conf"}, "done": {"0"}})
	if res.StatusCode != 303 {
		t.Errorf("uncheck = %d", res.StatusCode)
	}
	got, info = e.fileOf("servers/db1.example.com/todo.md")
	if got != "# To-do\n- [ ] Back up pg_hba.conf\n- [x] Upgrade to Debian 12\n" || info.Version != 3 {
		t.Errorf("after uncheck: %q", got)
	}
	// A toggle that changes nothing is no new version.
	e.req("alice", "POST", p, url.Values{"action": {"toggle"}, "text": {"nope"}, "done": {"1"}})
	if _, info = e.fileOf("servers/db1.example.com/todo.md"); info.Version != 3 {
		t.Errorf("no-op toggle made version %d", info.Version)
	}
	res, _ = e.req("bob", "POST", p, url.Values{"action": {"add"}, "text": {"Vacuum <b>now</b>"}})
	if res.StatusCode != 303 {
		t.Errorf("add = %d", res.StatusCode)
	}
	got, info = e.fileOf("servers/db1.example.com/todo.md")
	if !strings.HasSuffix(got, "- [x] Upgrade to Debian 12\n- [ ] Vacuum <b>now</b>\n") || info.UpdatedBy != "bob" || info.Version != 4 {
		t.Errorf("after add: %q %+v", got, info)
	}
	_, body = e.get("viewer", "/hosts/db1.example.com")
	if !strings.Contains(body, "Vacuum &lt;b&gt;now&lt;/b&gt;") || strings.Contains(body, "<b>now</b>") {
		t.Error("added to-do not escaped")
	}
	_, body = e.get("viewer", "/")
	if !strings.Contains(body, "Vacuum &lt;b&gt;now&lt;/b&gt;") || strings.Contains(body, "Upgrade to Debian 12") {
		t.Error("fleet page does not follow the edit")
	}
	// A host without todo.md gets one.
	res, _ = e.req("alice", "POST", "/hosts/web1.example.com/todos", url.Values{"action": {"add"}, "text": {"Rotate logs"}})
	if res.StatusCode != 303 {
		t.Errorf("add on a new file = %d", res.StatusCode)
	}
	got, info = e.fileOf("servers/web1.example.com/todo.md")
	if got != "# To-do\n- [ ] Rotate logs\n" || info.Version != 1 {
		t.Errorf("new todo.md: %q", got)
	}
	// The lint applies to UI edits too.
	tok := "gh" + "p_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	res, body = e.req("alice", "POST", "/hosts/web1.example.com/todos", url.Values{"action": {"add"}, "text": {"token " + tok}})
	if res.StatusCode != 422 || strings.Contains(body, tok) {
		t.Errorf("lint on an edit = %d (echoed secret: %v)", res.StatusCode, strings.Contains(body, tok))
	}
	if got, _ = e.fileOf("servers/web1.example.com/todo.md"); strings.Contains(got, tok) {
		t.Error("a refused edit was stored")
	}
	// Empty, multi-line and unknown hosts are refused.
	for _, f := range []url.Values{{"action": {"add"}, "text": {"  "}}, {"action": {"add"}, "text": {"a\nb"}}, {"action": {"x"}, "text": {"a"}}} {
		if res, _ = e.req("alice", "POST", p, f); res.StatusCode != 400 {
			t.Errorf("bad form %v = %d", f, res.StatusCode)
		}
	}
	if res, _ = e.req("alice", "POST", "/hosts/nope.example.com/todos", url.Values{"action": {"add"}, "text": {"x"}}); res.StatusCode != 404 {
		t.Errorf("unknown host = %d", res.StatusCode)
	}
}

func TestFactEdits(t *testing.T) {
	e := newEnv(t)
	p := "/hosts/web1.example.com/facts"
	if res, _ := e.req("viewer", "POST", p, url.Values{"owner": {"x"}, "tags": {"y"}}); res.StatusCode != 403 {
		t.Errorf("viewer facts = %d", res.StatusCode)
	}
	res, _ := e.req("bob", "POST", p, url.Values{"owner": {"bob"}, "tags": {"prod, web, <i>x</i>"}})
	if res.StatusCode != 303 {
		t.Fatalf("facts = %d", res.StatusCode)
	}
	got, info := e.fileOf("servers/web1.example.com/memory.md")
	if !strings.Contains(got, "- Owner: bob\n- Services: nginx 1.26") || !strings.Contains(got, "- Tags: prod, web, <i>x</i>\n- SSH port: 2222") ||
		strings.Contains(got, "alice") || info.Version != 2 || info.UpdatedBy != "bob" {
		t.Errorf("memory.md after facts: %q %+v", got, info)
	}
	if strings.Count(got, "\n") != 10 {
		t.Errorf("line count changed: %q", got)
	}
	_, body := e.get("viewer", "/")
	if !strings.Contains(body, "<td>bob</td>") || !strings.Contains(body, "&lt;i&gt;x&lt;/i&gt;") || strings.Contains(body, "<i>x</i>") {
		t.Error("fleet does not show the new owner and escaped tag")
	}
	// A host without the lines gets them after its facts; an
	// empty value removes the line.
	res, _ = e.req("alice", "POST", "/hosts/mac.local/facts", url.Values{"owner": {"carol"}, "tags": {""}})
	if res.StatusCode != 303 {
		t.Errorf("facts on mac = %d", res.StatusCode)
	}
	got, _ = e.fileOf("servers/mac.local/memory.md")
	if got != "# mac.local\n- OS: macOS 15.1 Sequoia\n- Mode: local\n- Last connected: yesterday\n- Owner: carol\n" {
		t.Errorf("mac memory.md: %q", got)
	}
	res, _ = e.req("alice", "POST", p, url.Values{"owner": {""}, "tags": {"web"}})
	got, _ = e.fileOf("servers/web1.example.com/memory.md")
	if strings.Contains(got, "Owner:") || !strings.Contains(got, "- Tags: web\n") {
		t.Errorf("owner not removed: %q", got)
	}
	for _, f := range []url.Values{{"owner": {"a\nb"}, "tags": {""}}, {"owner": {strings.Repeat("x", 201)}, "tags": {""}}} {
		if res, _ = e.req("alice", "POST", p, f); res.StatusCode != 400 {
			t.Errorf("bad facts %v = %d", f, res.StatusCode)
		}
	}
	// A host whose memory.md was never pushed: 404, no file made.
	e.st.DeleteFile("servers/mac.local/memory.md")
	if res, _ = e.req("alice", "POST", "/hosts/mac.local/facts", url.Values{"owner": {"x"}}); res.StatusCode != 404 {
		t.Errorf("facts without memory.md = %d", res.StatusCode)
	}
}

func TestLinkEdits(t *testing.T) {
	e := newEnv(t)
	if res, _ := e.req("viewer", "POST", "/links", url.Values{"action": {"add"}, "from": {"a.example.com"}, "to": {"b.example.com"}}); res.StatusCode != 403 {
		t.Errorf("viewer link = %d", res.StatusCode)
	}
	_, body := e.get("viewer", "/")
	if !strings.Contains(body, "web1.example.com -&gt; db1.example.com") && !strings.Contains(body, "web1.example.com → db1.example.com") {
		t.Error("fleet lacks the existing link")
	}
	if strings.Contains(body, `action="/links"`) {
		t.Error("viewer sees the link form")
	}
	_, body = e.get("alice", "/")
	if !strings.Contains(body, `action="/links"`) {
		t.Error("operator sees no link form")
	}
	res, _ := e.req("alice", "POST", "/links", url.Values{"action": {"add"}, "from": {"db1.example.com"}, "to": {"backup.example.com"}, "label": {"ssh <b>"}})
	if res.StatusCode != 303 || res.Header.Get("Location") != "/" {
		t.Fatalf("add link = %d", res.StatusCode)
	}
	got, info := e.fileOf("network.md")
	if !strings.HasSuffix(got, "- Link: web1.example.com -> db1.example.com (postgresql 5432)\n- Link: db1.example.com -> backup.example.com (ssh <b>)\n") || info.Version != 2 || info.UpdatedBy != "alice" {
		t.Errorf("network.md after add: %q %+v", got, info)
	}
	links, _ := e.st.Links()
	if len(links) != 2 || links[1].Label != "ssh <b>" {
		t.Errorf("links %+v", links)
	}
	_, body = e.get("viewer", "/")
	if !strings.Contains(body, "ssh &lt;b&gt;") || strings.Contains(body, "ssh <b>") {
		t.Error("link label not escaped")
	}
	res, _ = e.req("bob", "POST", "/links", url.Values{"action": {"remove"}, "from": {"web1.example.com"}, "to": {"db1.example.com"}, "label": {"postgresql 5432"}})
	if res.StatusCode != 303 {
		t.Errorf("remove link = %d", res.StatusCode)
	}
	got, info = e.fileOf("network.md")
	if got != "# Network\nAll servers sit in one VLAN; db1 is reached over the private IP.\n- Link: db1.example.com -> backup.example.com (ssh <b>)\n" || info.Version != 3 {
		t.Errorf("network.md after remove: %q", got)
	}
	// A link without a label; a bad host; a label with parens.
	res, _ = e.req("alice", "POST", "/links", url.Values{"action": {"add"}, "from": {"a.example.com"}, "to": {"b.example.com"}})
	got, _ = e.fileOf("network.md")
	if res.StatusCode != 303 || !strings.HasSuffix(got, "- Link: a.example.com -> b.example.com\n") {
		t.Errorf("unlabeled link: %d %q", res.StatusCode, got)
	}
	for _, f := range []url.Values{
		{"action": {"add"}, "from": {"a<b"}, "to": {"b.example.com"}},
		{"action": {"add"}, "from": {"a.example.com"}, "to": {""}},
		{"action": {"add"}, "from": {"a.example.com"}, "to": {"b.example.com"}, "label": {"x (y)"}},
		{"action": {"drop"}, "from": {"a.example.com"}, "to": {"b.example.com"}},
	} {
		if res, _ = e.req("alice", "POST", "/links", f); res.StatusCode != 400 {
			t.Errorf("bad link %v = %d", f, res.StatusCode)
		}
	}
	// Without network.md the first link creates it.
	e.st.DeleteFile("network.md")
	res, _ = e.req("alice", "POST", "/links", url.Values{"action": {"add"}, "from": {"a.example.com"}, "to": {"b.example.com"}, "label": {"x"}})
	got, info = e.fileOf("network.md")
	if res.StatusCode != 303 || got != "# Network\n- Link: a.example.com -> b.example.com (x)\n" {
		t.Errorf("network.md created: %d %q", res.StatusCode, got)
	}
}
