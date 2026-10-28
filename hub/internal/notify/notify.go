// Package notify sends a text through the channels of
// rules/notify.md: ntfy, Matrix and a Slack-compatible webhook.
// The secrets (an Authorization header, the webhook URL) live in
// files the admin places next to the database, mode 600. They
// are read at send time, never stored and never logged: an error
// names the channel and the HTTP status, nothing else.
package notify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
)

// Message is one notification: a title (ntfy shows it; the
// others get the text alone) and the text.
type Message struct {
	Title string
	Text  string
}

// Sender is one channel.
type Sender interface {
	Name() string
	Send(ctx context.Context, m Message) error
}

// Logf is where warnings and retries go; log.Printf in the hub.
type Logf func(format string, a ...any)

// RetryDelay is the pause before the one retry a failed send
// gets. Tests set it to zero.
var RetryDelay = 10 * time.Second

// Timeout bounds one HTTP request.
const Timeout = 20 * time.Second

// Result is the outcome of one channel in Deliver.
type Result struct {
	Channel  string
	Attempts int
	Err      error
}

// Deliver sends m through every sender. A failed send is logged
// and retried once; the final outcome per channel comes back.
func Deliver(ctx context.Context, senders []Sender, m Message, logf Logf) []Result {
	out := make([]Result, 0, len(senders))
	for _, s := range senders {
		r := Result{Channel: s.Name()}
		for r.Attempts = 1; r.Attempts <= 2; r.Attempts++ {
			r.Err = s.Send(ctx, m)
			if r.Err == nil {
				break
			}
			if r.Attempts == 1 {
				logf("notify: %s failed (%v), retry in %s", s.Name(), r.Err, RetryDelay)
				select {
				case <-time.After(RetryDelay):
				case <-ctx.Done():
					r.Attempts++
					r.Err = ctx.Err()
				}
			} else {
				logf("notify: %s failed again (%v); giving up", s.Name(), r.Err)
			}
		}
		if r.Attempts > 2 {
			r.Attempts = 2
		}
		out = append(out, r)
	}
	return out
}

// FromConfig builds the enabled channels in the order ntfy,
// matrix, webhook.
func FromConfig(c config.Notify) []Sender {
	var out []Sender
	if c.Ntfy.Enabled() {
		out = append(out, &Ntfy{Server: c.Ntfy.Server, Topic: c.Ntfy.Topic, HeaderFile: c.Ntfy.HeaderFile})
	}
	if c.Matrix.Enabled() {
		out = append(out, &Matrix{Server: c.Matrix.Server, Room: c.Matrix.Room, HeaderFile: c.Matrix.HeaderFile})
	}
	if c.Webhook.Enabled() {
		out = append(out, &Webhook{CurlrcFile: c.Webhook.CurlrcFile})
	}
	return out
}

// ByName picks one sender; nil when it is not configured.
func ByName(ss []Sender, name string) Sender {
	for _, s := range ss {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// ---- secret files ------------------------------------------

// Header is one line of a header file.
type Header struct {
	Name, Value string
}

// warnMode logs when a secret file is readable by group or
// others. The hub does not refuse: an admin may have chosen a
// group on purpose; the log says what it saw.
func warnMode(path string, logf Logf) {
	st, err := os.Stat(path)
	if err != nil || logf == nil {
		return
	}
	if m := st.Mode().Perm(); m&0o077 != 0 {
		logf("notify: %s has mode %04o; a secret file should be 0600", path, m)
	}
}

// readHeaders reads "Name: value" lines; blank lines and # lines
// are skipped, a line without a colon too.
func readHeaders(path string, logf Logf) ([]Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("header file %s: %w", path, errors.Unwrap(err))
	}
	defer f.Close()
	warnMode(path, logf)
	var out []Header
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out = append(out, Header{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)})
	}
	return out, sc.Err()
}

// webhookURL reads the url line of a curlrc: `url = "…"`, `url
// "…"`, `url=…` or `--url …` (curl's manual accepts them all).
func webhookURL(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("curlrc %s: %w", path, errors.Unwrap(err))
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		key := line
		rest := ""
		if i := strings.IndexAny(line, " \t=:"); i >= 0 {
			key, rest = line[:i], line[i+1:]
		}
		if strings.TrimLeft(key, "-") != "url" {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
		rest = strings.TrimSpace(strings.Trim(rest, `"`))
		u, err := url.Parse(rest)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "", fmt.Errorf("curlrc %s: the url line is no http(s) URL", path)
		}
		return rest, nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("curlrc %s: no url line", path)
}

// segment percent-encodes one path segment strictly: everything
// but the unreserved characters of RFC 3986. url.PathEscape
// would leave ":" alone; the Matrix spec's example wants %3A.
func segment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ---- http --------------------------------------------------

var client = &http.Client{Timeout: Timeout}

// do sends one request and turns anything but 2xx into an error
// that names the channel and the status. A transport error is
// stripped of the URL it would otherwise carry.
func do(ctx context.Context, channel string, req *http.Request) error {
	req = req.WithContext(ctx)
	res, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("%s: %v", channel, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%s: HTTP %d", channel, res.StatusCode)
	}
	return nil
}

// ---- ntfy --------------------------------------------------

// Ntfy publishes the text as the body of a POST to
// <server>/<topic> with a Title header; verified against
// https://docs.ntfy.sh/publish/ on 2026-10-28 by rules/notify.md.
type Ntfy struct {
	Server     string
	Topic      string
	HeaderFile string // optional
	Logf       Logf
}

// Name is "ntfy".
func (n *Ntfy) Name() string { return "ntfy" }

// Send publishes one message.
func (n *Ntfy) Send(ctx context.Context, m Message) error {
	req, err := http.NewRequest("POST", strings.TrimRight(n.Server, "/")+"/"+segment(n.Topic), strings.NewReader(m.Text))
	if err != nil {
		return fmt.Errorf("ntfy: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Title", m.Title)
	if n.HeaderFile != "" {
		hs, err := readHeaders(n.HeaderFile, n.Logf)
		if err != nil {
			return fmt.Errorf("ntfy: %v", err)
		}
		for _, h := range hs {
			req.Header.Set(h.Name, h.Value)
		}
	}
	return do(ctx, "ntfy", req)
}

// ---- matrix ------------------------------------------------

// Matrix PUTs an m.room.message event; verified against
// https://spec.matrix.org/latest/client-server-api/ on
// 2026-10-28 by rules/notify.md. The transaction id is the time
// plus a counter, unique per process.
type Matrix struct {
	Server     string
	Room       string
	HeaderFile string
	Logf       Logf
	counter    atomic.Uint64
}

// Name is "matrix".
func (x *Matrix) Name() string { return "matrix" }

// Send posts one m.text event.
func (x *Matrix) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(map[string]string{"msgtype": "m.text", "body": m.Text})
	if err != nil {
		return fmt.Errorf("matrix: %v", err)
	}
	txn := fmt.Sprintf("jolitz-hub-%d-%d", time.Now().UnixNano(), x.counter.Add(1))
	u := strings.TrimRight(x.Server, "/") + "/_matrix/client/v3/rooms/" + segment(x.Room) + "/send/m.room.message/" + txn
	req, err := http.NewRequest("PUT", u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("matrix: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	hs, err := readHeaders(x.HeaderFile, x.Logf)
	if err != nil {
		return fmt.Errorf("matrix: %v", err)
	}
	for _, h := range hs {
		req.Header.Set(h.Name, h.Value)
	}
	return do(ctx, "matrix", req)
}

// ---- webhook -----------------------------------------------

// Webhook POSTs {"text": …} to the url line of a curlrc file;
// verified against Slack's incoming-webhook documentation on
// 2026-10-28 by rules/notify.md. The URL is the secret.
type Webhook struct {
	CurlrcFile string
	Logf       Logf
}

// Name is "webhook".
func (w *Webhook) Name() string { return "webhook" }

// Send posts one message.
func (w *Webhook) Send(ctx context.Context, m Message) error {
	u, err := webhookURL(w.CurlrcFile)
	if err != nil {
		return fmt.Errorf("webhook: %v", err)
	}
	warnMode(w.CurlrcFile, w.Logf)
	body, err := json.Marshal(map[string]string{"text": m.Text})
	if err != nil {
		return fmt.Errorf("webhook: %v", err)
	}
	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		return errors.New("webhook: the url line is no usable URL")
	}
	req.Header.Set("Content-Type", "application/json")
	return do(ctx, "webhook", req)
}
