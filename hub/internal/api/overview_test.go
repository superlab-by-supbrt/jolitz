package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
)

// newEnvWith builds the server with a configuration and mounts
// /metrics next to /api/v1/, as main does.
func newEnvWith(t *testing.T, cfg *config.Config) *env {
	t.Helper()
	e := newEnv(t)
	e.srv.Close()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", New(Config{Store: e.st, Version: "test", Settings: cfg}))
	mux.Handle("/metrics", Metrics(Config{Store: e.st, Version: "test", Settings: cfg}))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) raw(user, method, path, body string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func TestOverviewJSON(t *testing.T) {
	e := newEnvWith(t, config.Default())
	res, body := e.do("op", "PUT", "files/servers/web1.example.com/memory.md", memoryMD)
	e.want(201, res, body, "put memory")
	res, body = e.do("op", "PUT", "files/servers/web1.example.com/todo.md", "- [ ] rotate the key\n")
	e.want(201, res, body, "put todo")
	res, body = e.do("", "GET", "overview.json", "")
	e.want(401, res, body, "overview without token")
	res, body = e.do("viewer", "GET", "overview.json", "")
	e.want(200, res, body, "overview")
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %s", ct)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"generated", "today", "days", "tag", "hosts", "todos", "changes", "network", "audit", "guard", "lint"} {
		if _, ok := top[k]; !ok {
			t.Errorf("overview.json lacks %s", k)
		}
	}
	if string(top["days"]) != "30" || string(top["today"]) != `"2026-10-28"` {
		t.Errorf("days %s today %s", top["days"], top["today"])
	}
	var hosts []struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
		Todos int    `json:"todos"`
		EOL   struct {
			State string `json:"state"`
		} `json:"eol"`
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	if err := json.Unmarshal(top["hosts"], &hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Name != "web1.example.com" || hosts[0].Owner != "alice" || hosts[0].Todos != 1 || hosts[0].EOL.State != "nodata" || len(hosts[0].Services) != 2 {
		t.Errorf("hosts %+v", hosts)
	}
	if string(top["lint"]) != "[]" {
		t.Errorf("lint %s", top["lint"])
	}
}

func TestMetrics(t *testing.T) {
	cfg := config.Default()
	e := newEnvWith(t, cfg)
	res, body := e.do("op", "PUT", "files/servers/web1.example.com/memory.md", memoryMD)
	e.want(201, res, body, "put memory")
	res, body = e.raw("", "GET", "/metrics", "")
	e.want(401, res, body, "metrics without token")
	if strings.Contains(body, "jolitz_") {
		t.Error("401 carries metrics")
	}
	res, body = e.raw("viewer", "GET", "/metrics", "")
	e.want(200, res, body, "metrics with token")
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Errorf("content type %s", res.Header.Get("Content-Type"))
	}
	for _, w := range []string{"# TYPE jolitz_fleet_hosts gauge\njolitz_fleet_hosts 1\n", `jolitz_host_todos{host="web1.example.com"} 0`, `jolitz_host_last_connected_days{host="web1.example.com"} 0`, `jolitz_host_eol_days{host="web1.example.com"} -1`, "jolitz_guard_denials_30d 0", "jolitz_hub_sessions_open 0", "jolitz_hub_files 1", `jolitz_hub_last_push_age_seconds{user="op"} 0`} {
		if !strings.Contains(body, w) {
			t.Errorf("metrics lack %q", w)
		}
	}
	res, body = e.raw("", "POST", "/metrics", "")
	e.want(405, res, body, "post metrics")
	// metrics_public = true: no token needed.
	pub := config.Default()
	pub.MetricsPublic = true
	e2 := newEnvWith(t, pub)
	res, body = e2.raw("", "GET", "/metrics", "")
	e2.want(200, res, body, "public metrics")
	if !strings.Contains(body, "jolitz_fleet_hosts 0") {
		t.Errorf("public metrics body %s", body)
	}
}

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

func TestNotifyTest(t *testing.T) {
	cap := &capture{}
	recv := httptest.NewServer(cap)
	defer recv.Close()
	dir := t.TempDir()
	tok := "tk_" + strings.Repeat("y", 24)
	if err := os.WriteFile(filepath.Join(dir, "ntfy-auth"), []byte("Authorization: Bearer "+tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Notify.Ntfy = config.Ntfy{Server: recv.URL, Topic: "fleet", HeaderFile: filepath.Join(dir, "ntfy-auth")}
	notify.RetryDelay = 0
	e := newEnvWith(t, cfg)
	for _, c := range []struct {
		user, body string
		code       int
	}{
		{"", `{"channel":"ntfy"}`, 401},
		{"viewer", `{"channel":"ntfy"}`, 403},
		{"op", `{"channel":"ntfy"}`, 403},
		{"admin", `{"channel":"pigeon"}`, 400},
		{"admin", `{"channel":"matrix"}`, 404},
		{"admin", `nonsense`, 400},
		{"admin", `{"channel":"ntfy"}`, 200},
	} {
		res, body := e.do(c.user, "POST", "notify/test", c.body)
		e.want(c.code, res, body, c.user+" "+c.body)
		if strings.Contains(body, tok) || strings.Contains(body, recv.URL) {
			t.Errorf("response leaks: %s", body)
		}
	}
	if len(cap.hits) != 1 || cap.hits[0].URL.Path != "/fleet" || cap.hits[0].Header.Get("Authorization") != "Bearer "+tok {
		t.Fatalf("receiver got %d requests: %+v", len(cap.hits), cap.hits)
	}
	if !strings.Contains(cap.body[0], "test message") || !strings.Contains(cap.body[0], "admin") {
		t.Errorf("test message body %q", cap.body[0])
	}
	recv.Close()
	res, body := e.do("admin", "POST", "notify/test", `{"channel":"ntfy"}`)
	e.want(502, res, body, "receiver down")
	if strings.Contains(body, recv.URL) {
		t.Errorf("error leaks the URL: %s", body)
	}
}

func TestEndpointsDocumented(t *testing.T) {
	seen := map[string]bool{}
	for _, ep := range Endpoints {
		seen[ep.Method+" "+ep.Path] = true
	}
	for _, w := range []string{"GET overview.json", "POST notify/test"} {
		if !seen[w] {
			t.Errorf("%s missing from Endpoints", w)
		}
	}
}
