package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/jobs"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
)

type capture struct {
	mu   sync.Mutex
	hits []*http.Request
	body []string
}

func (c *capture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.hits = append(c.hits, r)
	c.body = append(c.body, string(b))
	c.mu.Unlock()
}

// newSettingsEnv wires a configuration with one ntfy channel
// whose header file holds a token, and a scheduler that ran once.
func newSettingsEnv(t *testing.T) (*env, *capture, string) {
	t.Helper()
	e := newEnv(t)
	e.srv.Close()
	cap := &capture{}
	recv := httptest.NewServer(cap)
	t.Cleanup(recv.Close)
	dir := t.TempDir()
	tok := "tk_" + strings.Repeat("z", 24)
	if err := os.WriteFile(filepath.Join(dir, "ntfy-auth"), []byte("Authorization: Bearer "+tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Path = filepath.Join(dir, "hub.toml")
	cfg.DataDir = dir
	cfg.Notify.Ntfy = config.Ntfy{Server: recv.URL, Topic: "fleet-a8f3", HeaderFile: filepath.Join(dir, "ntfy-auth")}
	notify.RetryDelay = 0
	sched := jobs.New(e.st, cfg, func(string, ...any) {})
	sched.Now = e.st.Now
	sched.EOLBase = recv.URL // answers 200 with an empty body: "no JSON", so the eol job fails visibly
	sched.RunDue(t.Context())
	e.srv = httptest.NewServer(New(Config{Store: e.st, Version: "1.2.3-test", Settings: cfg, Scheduler: sched}))
	t.Cleanup(e.srv.Close)
	return e, cap, tok
}

func TestSettingsPage(t *testing.T) {
	e, cap, tok := newSettingsEnv(t)
	for _, u := range []string{"viewer", "alice"} {
		res, body := e.get(u, "/settings")
		if res.StatusCode != 403 {
			t.Errorf("%s /settings = %d", u, res.StatusCode)
		}
		if strings.Contains(body, "fleet-a8f3") {
			t.Errorf("%s sees the configuration", u)
		}
		_, body = e.get(u, "/")
		if strings.Contains(body, `href="/settings"`) {
			t.Errorf("%s sees the settings link", u)
		}
	}
	res, body := e.get("root", "/settings")
	if res.StatusCode != 200 {
		t.Fatalf("admin /settings = %d", res.StatusCode)
	}
	for _, w := range []string{
		"<th>listen</th><td>127.0.0.1:7331</td>", "<th>retention.sessions_days</th><td>90</td>", "<th>summary.weekday</th><td>mon</td>",
		"<th>notify.ntfy.topic</th><td>fleet-a8f3</td>", "ntfy-auth", "hub.toml",
		"<td>retention</td>", "<td>eol</td>", "<td>summary</td>", `<span class="pill ok">ok</span>`, `<span class="pill bad">failed</span>`,
		"armed; next mon 07:00", "0 sessions, 0 guard rows, 0 old file versions removed",
		`<input type="hidden" name="channel" value="ntfy">`, "send test message", "not configured", "token-protected", `href="/settings" class="on"`,
	} {
		if !strings.Contains(body, w) {
			t.Errorf("settings lacks %q", w)
		}
	}
	if strings.Contains(body, tok) {
		t.Error("settings page shows the token")
	}
	if strings.Contains(body, `name="channel" value="matrix"`) {
		t.Error("test button for an unconfigured channel")
	}
	// Test send: admin only, one request at the receiver, the
	// outcome on the page.
	res, _ = e.req("alice", "POST", "/settings/test", url.Values{"channel": {"ntfy"}})
	if res.StatusCode != 403 {
		t.Errorf("operator test = %d", res.StatusCode)
	}
	res, _ = e.req("root", "POST", "/settings/test", url.Values{"channel": {"pigeon"}})
	if res.StatusCode != 400 {
		t.Errorf("bad channel = %d", res.StatusCode)
	}
	res, body = e.req("root", "POST", "/settings/test", url.Values{"channel": {"matrix"}})
	if res.StatusCode != 404 || !strings.Contains(body, "matrix is not configured") {
		t.Errorf("unconfigured channel = %d", res.StatusCode)
	}
	res, body = e.req("root", "POST", "/settings/test", url.Values{"channel": {"ntfy"}})
	if res.StatusCode != 200 || !strings.Contains(body, "test message sent via ntfy") {
		t.Errorf("test send = %d", res.StatusCode)
	}
	// The receiver also saw the eol probes (one per product);
	// exactly one request is the test message.
	var topic []int
	for i, h := range cap.hits {
		if h.URL.Path == "/fleet-a8f3" {
			topic = append(topic, i)
		}
	}
	if len(topic) != 1 {
		t.Fatalf("receiver got %d topic requests of %d", len(topic), len(cap.hits))
	}
	hit := cap.hits[topic[0]]
	if hit.Header.Get("Authorization") != "Bearer "+tok || hit.Header.Get("Title") != "jolitz hub test" || !strings.Contains(cap.body[topic[0]], "sent by root") {
		t.Errorf("test message request %v %q", hit.Header, cap.body[topic[0]])
	}
	if strings.Contains(body, tok) {
		t.Error("result page shows the token")
	}
}

func TestSettingsWithoutConfig(t *testing.T) {
	e := newEnv(t)
	res, body := e.get("root", "/settings")
	if res.StatusCode != 200 {
		t.Fatalf("/settings = %d", res.StatusCode)
	}
	if !strings.Contains(body, "defaults; no hub.toml") || !strings.Contains(body, "No scheduler is running") {
		t.Error("settings without config lacks the default notes")
	}
	res, _ = e.get("", "/settings")
	if res.StatusCode != 303 {
		t.Errorf("/settings without cookie = %d", res.StatusCode)
	}
}
