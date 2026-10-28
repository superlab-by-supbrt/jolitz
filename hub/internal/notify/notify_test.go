package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
)

// The token is assembled at run time so the repository never
// holds a token-shaped literal.
var token = "tk_" + strings.Repeat("x", 20) + "secret"

func writeFile(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

type capture struct {
	mu   sync.Mutex
	reqs []*http.Request
	body []string
	fail int // HTTP 500 for the first n requests
}

func (c *capture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs = append(c.reqs, r)
		c.body = append(c.body, string(b))
		n := len(c.reqs)
		c.mu.Unlock()
		if n <= c.fail {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"event_id":"$1"}`)
	}
}

var msg = Message{Title: "jolitz fleet summary", Text: "jolitz fleet summary, 2026-10-28\nHosts: 3\n"}

func TestNtfy(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()
	hdr := writeFile(t, t.TempDir(), "ntfy-auth", "Authorization: Bearer "+token+"\n", 0o600)
	s := &Ntfy{Server: srv.URL, Topic: "jolitz-fleet-a8f3", HeaderFile: hdr}
	if s.Name() != "ntfy" {
		t.Error(s.Name())
	}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	r := c.reqs[0]
	if r.Method != "POST" || r.URL.Path != "/jolitz-fleet-a8f3" {
		t.Errorf("%s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Title") != "jolitz fleet summary" || r.Header.Get("Authorization") != "Bearer "+token {
		t.Errorf("headers %v", r.Header)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") || c.body[0] != msg.Text {
		t.Errorf("body %q type %s", c.body[0], r.Header.Get("Content-Type"))
	}
	// Without a header file the topic is published unauthenticated.
	s2 := &Ntfy{Server: srv.URL, Topic: "t"}
	if err := s2.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if c.reqs[1].Header.Get("Authorization") != "" {
		t.Error("Authorization sent without a header file")
	}
	// A missing header file is an error that names the path, not
	// a silent unauthenticated publish.
	s3 := &Ntfy{Server: srv.URL, Topic: "t", HeaderFile: filepath.Join(t.TempDir(), "gone")}
	if err := s3.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("missing header file: %v", err)
	}
}

func TestMatrix(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()
	hdr := writeFile(t, t.TempDir(), "matrix-auth", "# access token\nAuthorization: Bearer "+token+"\n\n", 0o600)
	s := &Matrix{Server: srv.URL, Room: "!abc123:example.org", HeaderFile: hdr}
	for i := 0; i < 2; i++ {
		if err := s.Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}
	r := c.reqs[0]
	prefix := "/_matrix/client/v3/rooms/%21abc123%3Aexample.org/send/m.room.message/"
	if r.Method != "PUT" || !strings.HasPrefix(r.URL.EscapedPath(), prefix) {
		t.Errorf("%s %s", r.Method, r.URL.EscapedPath())
	}
	txn1 := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
	txn2 := strings.TrimPrefix(c.reqs[1].URL.EscapedPath(), prefix)
	if txn1 == "" || txn1 == txn2 {
		t.Errorf("txn ids %q %q", txn1, txn2)
	}
	if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers %v", r.Header)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(c.body[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["msgtype"] != "m.text" || body["body"] != msg.Text || len(body) != 2 {
		t.Errorf("body %v", body)
	}
}

func TestWebhook(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()
	dir := t.TempDir()
	rc := writeFile(t, dir, "webhook.curlrc", "# slack\nurl = \""+srv.URL+"/services/T0/B0/XX\"\nsilent\n", 0o600)
	s := &Webhook{CurlrcFile: rc}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	r := c.reqs[0]
	if r.Method != "POST" || r.URL.Path != "/services/T0/B0/XX" || r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("%s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(c.body[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["text"] != msg.Text || len(body) != 1 {
		t.Errorf("body %v", body)
	}
	for _, in := range []string{"url=" + srv.URL + "/a\n", "url " + srv.URL + "/a\n", "--url \"" + srv.URL + "/a\"\n"} {
		u, err := webhookURL(writeFile(t, dir, "rc2", in, 0o600))
		if err != nil || u != srv.URL+"/a" {
			t.Errorf("%q: %q %v", in, u, err)
		}
	}
	if _, err := webhookURL(writeFile(t, dir, "rc3", "silent\n", 0o600)); err == nil || !strings.Contains(err.Error(), "no url") {
		t.Errorf("curlrc without url: %v", err)
	}
	if _, err := webhookURL(writeFile(t, dir, "rc4", "url = \"ftp://x\"\n", 0o600)); err == nil {
		t.Error("non-http url accepted")
	}
}

func TestRetryAndSecrecy(t *testing.T) {
	c := &capture{fail: 1}
	srv := httptest.NewServer(c.handler())
	defer srv.Close()
	RetryDelay = 0
	var logged []string
	logf := func(format string, a ...any) { logged = append(logged, fmt.Sprintf(format, a...)) }
	s := &Ntfy{Server: srv.URL, Topic: "t"}
	res := Deliver(context.Background(), []Sender{s}, msg, logf)
	if len(res) != 1 || res[0].Err != nil || res[0].Attempts != 2 {
		t.Errorf("results %+v", res)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "ntfy") || !strings.Contains(logged[0], "HTTP 500") || !strings.Contains(logged[0], "retry") {
		t.Errorf("log %q", logged)
	}
	// Always failing: two attempts, the error names the status,
	// never the URL or a header.
	c2 := &capture{fail: 99}
	srv2 := httptest.NewServer(c2.handler())
	defer srv2.Close()
	rc := writeFile(t, t.TempDir(), "rc", "url = \""+srv2.URL+"/services/T0/B0/SECRET\"\n", 0o600)
	w := &Webhook{CurlrcFile: rc}
	res = Deliver(context.Background(), []Sender{w}, msg, logf)
	if len(res) != 1 || res[0].Err == nil || res[0].Attempts != 2 || len(c2.reqs) != 2 {
		t.Fatalf("results %+v, %d requests", res, len(c2.reqs))
	}
	if !strings.Contains(res[0].Err.Error(), "HTTP 500") || strings.Contains(res[0].Err.Error(), "SECRET") || strings.Contains(res[0].Err.Error(), srv2.URL) {
		t.Errorf("error %v", res[0].Err)
	}
	for _, l := range logged {
		if strings.Contains(l, "SECRET") || strings.Contains(l, srv2.URL) || strings.Contains(l, token) {
			t.Errorf("log leaks: %s", l)
		}
	}
	// A transport error must not carry the URL either (url.Error
	// does by default).
	srv2.Close()
	err := w.Send(context.Background(), msg)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), srv2.URL) {
		t.Errorf("transport error leaks: %v", err)
	}
}

func TestHeaderFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "h", "Authorization: Bearer "+token+"\nX-Extra:  two words \n# comment\n\nnocolon\n", 0o600)
	var warned []string
	hs, err := readHeaders(p, func(format string, a ...any) { warned = append(warned, fmt.Sprintf(format, a...)) })
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Name != "Authorization" || hs[0].Value != "Bearer "+token || hs[1].Name != "X-Extra" || hs[1].Value != "two words" {
		t.Errorf("headers %+v", hs)
	}
	if len(warned) != 0 {
		t.Errorf("warned on a mode-600 file: %v", warned)
	}
	p2 := writeFile(t, dir, "loose", "Authorization: Bearer x\n", 0o644)
	if _, err := readHeaders(p2, func(format string, a ...any) { warned = append(warned, fmt.Sprintf(format, a...)) }); err != nil {
		t.Fatal(err)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "loose") || !strings.Contains(warned[0], "0644") {
		t.Errorf("no warning on a group/other-readable file: %v", warned)
	}
}

func TestFromConfig(t *testing.T) {
	c := config.Default()
	if len(FromConfig(c.Notify)) != 0 {
		t.Error("senders without configuration")
	}
	c.Notify.Ntfy.Topic = "t"
	c.Notify.Matrix = config.Matrix{Server: "https://m", Room: "!r:x", HeaderFile: "/h"}
	c.Notify.Webhook.CurlrcFile = "/w"
	ss := FromConfig(c.Notify)
	if len(ss) != 3 || ss[0].Name() != "ntfy" || ss[1].Name() != "matrix" || ss[2].Name() != "webhook" {
		t.Errorf("senders %v", ss)
	}
	if ByName(ss, "matrix") == nil || ByName(ss, "pigeon") != nil {
		t.Error("ByName")
	}
}
