package jobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func newStore(t *testing.T, c *clock) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Now = c.now
	t.Cleanup(func() { st.Close() })
	return st
}

var quiet = func(string, ...any) {}

func TestRetention(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	st := newStore(t, c)
	old := c.now().AddDate(0, 0, -100)
	c.set(old)
	id, _ := st.StartSession("alice", "web1.example.com", "mbp")
	_ = st.EndSession(id)
	_ = st.AddGuard("alice", old, "old")
	c.set(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	_ = st.AddGuard("bob", c.now(), "new")
	cfg := config.Default()
	cfg.EOL.Enabled = false
	s := New(st, cfg, quiet)
	s.Now = c.now
	if len(s.Jobs) != 1 || s.Jobs[0].Name != "retention" {
		t.Fatalf("jobs %v", names(s))
	}
	s.RunDue(context.Background())
	r, _ := st.JobRun("retention")
	if r == nil || !r.OK || r.Result != "1 sessions, 1 guard rows removed; file versions: not in phase 1" {
		t.Errorf("run %+v", r)
	}
	if _, _, err := st.Session(id); err == nil {
		t.Error("old session kept")
	}
	gs, _ := st.GuardCounts(time.Time{})
	if len(gs) != 1 || gs[0].Reason != "new" {
		t.Errorf("guard %+v", gs)
	}
	// Not due again within the day, due after it.
	c.set(c.now().Add(23 * time.Hour))
	s.RunDue(context.Background())
	if r2, _ := st.JobRun("retention"); !r2.At.Equal(r.At) {
		t.Error("retention ran twice in a day")
	}
	c.set(c.now().Add(2 * time.Hour))
	s.RunDue(context.Background())
	if r3, _ := st.JobRun("retention"); r3.At.Equal(r.At) || r3.Result != "0 sessions, 0 guard rows removed; file versions: not in phase 1" {
		t.Errorf("second run %+v", r3)
	}
	d := s.Describe()
	if len(d) != 1 || d[0].Name != "retention" || !strings.Contains(d[0].Schedule, "daily") || d[0].Last == nil {
		t.Errorf("describe %+v", d)
	}
}

func names(s *Scheduler) []string {
	var out []string
	for _, j := range s.Jobs {
		out = append(out, j.Name)
	}
	return out
}

func TestEOL(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	st := newStore(t, c)
	for p, b := range map[string]string{
		"servers/web1.example.com/memory.md": "# web1\n- OS: Debian 12\n",
		"servers/db1.example.com/memory.md":  "# db1\n- OS: Debian 11\n",
		"servers/mac.local/memory.md":        "# mac\n- OS: macOS 15.1\n",
		"servers/odd.example.com/memory.md":  "# odd\n- OS: Plan 9\n",
	} {
		if _, err := st.PutFile(p, []byte(b), "alice", ""); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var paths []string
	debian := `{"result":{"releases":[{"name":"12","eolFrom":"2028-06-30"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.RequestURI())
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/products/debian/":
			fmt.Fprint(w, debian)
		default:
			http.Error(w, "no", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	cfg := config.Default()
	s := New(st, cfg, quiet)
	s.Now = c.now
	s.EOLBase = srv.URL
	s.RunDue(context.Background())
	r, _ := st.JobRun("eol")
	if r == nil || r.OK || r.Result != "1 of 2 products fetched; macos: HTTP 503 (cache kept)" {
		t.Errorf("run %+v", r)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/products/debian/" || paths[1] != "/api/v1/products/macos/" {
		t.Errorf("requests %v", paths)
	}
	rows, _ := st.EOL()
	if len(rows) != 1 || rows[0].Product != "debian" || rows[0].Fetched != "2026-10-28" || string(rows[0].Body) != debian {
		t.Errorf("cache %+v", rows)
	}
	// Next day: debian now answers garbage; the cache keeps the
	// old body and date.
	debian = `{"result":{"releases":[]}}`
	c.set(c.now().Add(25 * time.Hour))
	s.RunDue(context.Background())
	rows, _ = st.EOL()
	if len(rows) != 1 || rows[0].Fetched != "2026-10-28" || !strings.Contains(string(rows[0].Body), "2028-06-30") {
		t.Errorf("cache after bad answer %+v", rows)
	}
	r, _ = st.JobRun("eol")
	if r.OK || !strings.Contains(r.Result, "debian: no releases in the answer") {
		t.Errorf("second run %+v", r)
	}
}

type ntfyCapture struct {
	mu     sync.Mutex
	bodies []string
}

func (n *ntfyCapture) serve(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	n.mu.Lock()
	n.bodies = append(n.bodies, string(b))
	n.mu.Unlock()
}

func TestSummarySchedule(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	// A Tuesday.
	c := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, loc)}
	st := newStore(t, c)
	if _, err := st.PutFile("servers/web1.example.com/memory.md", []byte("# web1\n- OS: Debian 12\n- Owner: alice\n"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	cap := &ntfyCapture{}
	srv := httptest.NewServer(http.HandlerFunc(cap.serve))
	defer srv.Close()
	cfg := config.Default()
	cfg.EOL.Enabled = false
	cfg.Summary = config.Summary{Weekday: "mon", Hour: 7}
	cfg.Notify.Ntfy = config.Ntfy{Server: srv.URL, Topic: "fleet"}
	notify.RetryDelay = 0
	s := New(st, cfg, quiet)
	s.Now = c.now
	if len(s.Jobs) != 2 || s.Jobs[1].Name != "summary" {
		t.Fatalf("jobs %v", names(s))
	}
	// First pass arms the job without sending.
	s.RunDue(context.Background())
	r, _ := st.JobRun("summary")
	if r == nil || !r.OK || !strings.Contains(r.Result, "armed") || !strings.Contains(r.Result, "next mon 07:00") {
		t.Errorf("armed %+v", r)
	}
	if len(cap.bodies) != 0 {
		t.Fatal("sent on arming")
	}
	// Monday 06:59: not yet.
	c.set(time.Date(2026, 11, 2, 6, 59, 0, 0, loc))
	s.RunDue(context.Background())
	if len(cap.bodies) != 0 {
		t.Fatal("sent before the hour")
	}
	c.set(time.Date(2026, 11, 2, 7, 0, 30, 0, loc))
	s.RunDue(context.Background())
	if len(cap.bodies) != 1 || !strings.HasPrefix(cap.bodies[0], "jolitz fleet summary, 2026-11-02\nHosts: 1, open to-dos: 0, changes in 7 days: 0\n") {
		t.Fatalf("summary bodies %q", cap.bodies)
	}
	if !strings.Contains(cap.bodies[0], "Owners\n  web1.example.com: alice\n") {
		t.Errorf("summary text %q", cap.bodies[0])
	}
	r, _ = st.JobRun("summary")
	if r == nil || !r.OK || r.Result != "sent via ntfy" {
		t.Errorf("run %+v", r)
	}
	// A minute later: not again.
	c.set(c.now().Add(time.Minute))
	s.RunDue(context.Background())
	if len(cap.bodies) != 1 {
		t.Fatal("sent twice in one hour")
	}
	// The hub was down over the hour next week: it catches up.
	c.set(time.Date(2026, 11, 9, 15, 45, 0, 0, loc))
	s.RunDue(context.Background())
	if len(cap.bodies) != 2 || !strings.HasPrefix(cap.bodies[1], "jolitz fleet summary, 2026-11-09\n") {
		t.Fatalf("catch-up bodies %q", cap.bodies)
	}
	// A failing channel is recorded as such.
	srv.Close()
	c.set(time.Date(2026, 11, 16, 7, 5, 0, 0, loc))
	s.RunDue(context.Background())
	r, _ = st.JobRun("summary")
	if r == nil || r.OK || !strings.Contains(r.Result, "ntfy failed") {
		t.Errorf("failed run %+v", r)
	}
	if strings.Contains(r.Result, srv.URL) {
		t.Errorf("result leaks the URL: %s", r.Result)
	}
	d := s.Describe()
	if len(d) != 2 || d[1].Name != "summary" || d[1].Schedule != "weekly, mon 07:00 local time" {
		t.Errorf("describe %+v", d)
	}
}

func TestNoSummaryWithoutChannel(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	st := newStore(t, c)
	cfg := config.Default()
	s := New(st, cfg, quiet)
	for _, j := range s.Jobs {
		if j.Name == "summary" {
			t.Error("summary job without a channel")
		}
	}
	if s.Jobs[1].Name != "eol" {
		t.Errorf("jobs %v", names(s))
	}
}

func TestLastOccurrence(t *testing.T) {
	loc := time.FixedZone("X", -5*3600)
	for _, c := range []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 11, 2, 7, 0, 0, 0, loc), time.Date(2026, 11, 2, 7, 0, 0, 0, loc)},   // Monday at the hour
		{time.Date(2026, 11, 2, 6, 59, 0, 0, loc), time.Date(2026, 10, 26, 7, 0, 0, 0, loc)}, // Monday before it
		{time.Date(2026, 11, 5, 23, 0, 0, 0, loc), time.Date(2026, 11, 2, 7, 0, 0, 0, loc)},  // Thursday
	} {
		got := lastOccurrence(c.now, time.Monday, 7)
		if !got.Equal(c.want) {
			t.Errorf("%s: %s, want %s", c.now, got, c.want)
		}
	}
}

func TestRunLoopStops(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	st := newStore(t, c)
	cfg := config.Default()
	cfg.EOL.Enabled = false
	s := New(st, cfg, quiet)
	s.Now = c.now
	s.Tick = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s.Run(ctx)
	if r, _ := st.JobRun("retention"); r == nil {
		t.Error("the loop ran nothing")
	}
}
