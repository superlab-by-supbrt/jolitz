// Package jobs is the hub's scheduler: retention (sessions and
// guard denials), the daily endoflife.date fetch and the weekly
// fleet summary. One goroutine wakes every Tick, asks each job
// whether it is due and records the outcome in the store, where
// the settings page reads it. The clock is a field so tests
// drive it.
//
// Retention of file versions (retention.versions_years) is read
// from the config but has no job: phase 1 keeps one version per
// file, so there is nothing to expire yet.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/overview"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// Job is one scheduled task.
type Job struct {
	Name     string
	Schedule string // for the settings page
	// Due says whether to run now, given the last recorded run
	// (nil when there is none).
	Due func(now time.Time, last *store.JobRun) bool
	// ArmFirst: on the first pass without a record, store a
	// baseline instead of running (the weekly summary must not
	// go out at every first start).
	ArmFirst func(now time.Time) string
	// Run does the work and returns a one-line result.
	Run func(ctx context.Context, now time.Time) (string, error)
}

// Scheduler holds the jobs and the clock.
type Scheduler struct {
	Store *store.Store
	Now   func() time.Time
	Tick  time.Duration
	Logf  func(string, ...any)
	Jobs  []*Job

	// EOLBase is the endoflife.date origin; tests point it at a
	// local server.
	EOLBase string
	// HTTP is the client for the EOL fetch.
	HTTP *http.Client

	cfg     *config.Config
	senders []notify.Sender
}

// Description is one row of the settings page.
type Description struct {
	Name     string
	Schedule string
	Last     *store.JobRun
}

// New builds the scheduler for a configuration: retention
// always, eol when enabled, summary when a channel is configured.
func New(st *store.Store, cfg *config.Config, logf func(string, ...any)) *Scheduler {
	s := &Scheduler{Store: st, Now: time.Now, Tick: time.Minute, Logf: logf, EOLBase: "https://endoflife.date",
		HTTP: &http.Client{Timeout: 15 * time.Second}, cfg: cfg, senders: notify.FromConfig(cfg.Notify)}
	daily := func(now time.Time, last *store.JobRun) bool {
		return last == nil || now.Sub(last.At) >= 24*time.Hour
	}
	s.Jobs = append(s.Jobs, &Job{
		Name:     "retention",
		Schedule: fmt.Sprintf("daily; sessions %d days, guard denials %d days", cfg.Retention.SessionsDays, cfg.Retention.GuardDays),
		Due:      daily,
		Run:      s.retention,
	})
	if cfg.EOL.Enabled {
		s.Jobs = append(s.Jobs, &Job{
			Name:     "eol",
			Schedule: "daily; endoflife.date, product names only",
			Due:      daily,
			Run:      s.eol,
		})
	}
	if len(s.senders) > 0 {
		wd := time.Weekday(config.WeekdayIndex(cfg.Summary.Weekday))
		hour := cfg.Summary.Hour
		s.Jobs = append(s.Jobs, &Job{
			Name:     "summary",
			Schedule: fmt.Sprintf("weekly, %s %02d:00 local time", cfg.Summary.Weekday, hour),
			Due: func(now time.Time, last *store.JobRun) bool {
				sched := lastOccurrence(now, wd, hour)
				return last != nil && last.At.Before(sched)
			},
			ArmFirst: func(now time.Time) string {
				next := lastOccurrence(now, wd, hour).AddDate(0, 0, 7)
				return fmt.Sprintf("armed; next %s %02d:00 (%s)", cfg.Summary.Weekday, hour, next.Format("2006-01-02"))
			},
			Run: s.summary,
		})
	}
	return s
}

// lastOccurrence is the most recent weekday@hour:00 at or before
// now, in now's location.
func lastOccurrence(now time.Time, wd time.Weekday, hour int) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	for t.Weekday() != wd || t.After(now) {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// Run loops until ctx ends: one pass now, then one per Tick.
func (s *Scheduler) Run(ctx context.Context) {
	s.RunDue(ctx)
	tick := time.NewTicker(s.Tick)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.RunDue(ctx)
		}
	}
}

// RunDue runs every job that is due and records the outcome.
func (s *Scheduler) RunDue(ctx context.Context) {
	now := s.Now()
	for _, j := range s.Jobs {
		last, err := s.Store.JobRun(j.Name)
		if err != nil {
			s.Logf("jobs: %s: %v", j.Name, err)
			continue
		}
		if last == nil && j.ArmFirst != nil {
			if err := s.Store.SetJobRun(j.Name, now, true, j.ArmFirst(now)); err != nil {
				s.Logf("jobs: %s: %v", j.Name, err)
			}
			continue
		}
		if !j.Due(now, last) {
			continue
		}
		result, err := j.Run(ctx, now)
		ok := err == nil
		if err != nil {
			if result == "" {
				result = err.Error()
			}
			s.Logf("jobs: %s failed: %s", j.Name, result)
		} else {
			s.Logf("jobs: %s: %s", j.Name, result)
		}
		if err := s.Store.SetJobRun(j.Name, now, ok, result); err != nil {
			s.Logf("jobs: %s: %v", j.Name, err)
		}
	}
}

// Describe lists the jobs with their last run.
func (s *Scheduler) Describe() []Description {
	out := make([]Description, 0, len(s.Jobs))
	for _, j := range s.Jobs {
		d := Description{Name: j.Name, Schedule: j.Schedule}
		d.Last, _ = s.Store.JobRun(j.Name)
		out = append(out, d)
	}
	return out
}

// ---- retention ---------------------------------------------

func (s *Scheduler) retention(_ context.Context, now time.Time) (string, error) {
	ns, err := s.Store.PruneSessions(now.AddDate(0, 0, -s.cfg.Retention.SessionsDays))
	if err != nil {
		return "", err
	}
	ng, err := s.Store.PruneGuard(now.AddDate(0, 0, -s.cfg.Retention.GuardDays))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d sessions, %d guard rows removed; file versions: not in phase 1", ns, ng), nil
}

// ---- end of life -------------------------------------------

// eol fetches https://endoflife.date/api/v1/products/<p>/ for
// every product derived from memory (the mapping of
// parse.Memory, the same as bin/jolitz-overview's product()).
// The request carries the product name and nothing else. A
// failed fetch keeps the cached answer.
func (s *Scheduler) eol(ctx context.Context, now time.Time) (string, error) {
	hosts, err := s.Store.Hosts()
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	var products []string
	for _, h := range hosts {
		if h.Product != "" && !seen[h.Product] {
			seen[h.Product] = true
			products = append(products, h.Product)
		}
	}
	sort.Strings(products)
	if len(products) == 0 {
		return "no products in memory", nil
	}
	fetched := 0
	var failures []string
	for _, p := range products {
		if err := s.fetchEOL(ctx, p, now); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v (cache kept)", p, err))
			continue
		}
		fetched++
	}
	result := fmt.Sprintf("%d of %d products fetched", fetched, len(products))
	if len(failures) > 0 {
		result += "; " + strings.Join(failures, "; ")
		return result, errors.New("eol fetch incomplete")
	}
	return result, nil
}

func (s *Scheduler) fetchEOL(ctx context.Context, product string, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(s.EOLBase, "/")+"/api/v1/products/"+url.PathEscape(product)+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "jolitz-hub")
	res, err := s.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var v struct {
		Result struct {
			Releases []json.RawMessage `json:"releases"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return errors.New("answer is no JSON")
	}
	if len(v.Result.Releases) == 0 {
		return errors.New("no releases in the answer")
	}
	return s.Store.PutEOL(product, now.Format("2006-01-02"), body)
}

// ---- weekly summary ----------------------------------------

// summary sends bin/jolitz-overview --text, built by
// internal/overview, through every configured channel.
func (s *Scheduler) summary(ctx context.Context, _ time.Time) (string, error) {
	d, err := overview.Load(s.Store, 7)
	if err != nil {
		return "", err
	}
	text := d.Text()
	results := notify.Deliver(ctx, s.senders, notify.Message{Title: "jolitz fleet summary", Text: text}, s.Logf)
	var sent, failed []string
	for _, r := range results {
		if r.Err == nil {
			sent = append(sent, r.Channel)
		} else {
			failed = append(failed, fmt.Sprintf("%s failed (%v)", r.Channel, r.Err))
		}
	}
	var parts []string
	if len(sent) > 0 {
		parts = append(parts, "sent via "+strings.Join(sent, ", "))
	}
	parts = append(parts, failed...)
	result := strings.Join(parts, "; ")
	if len(failed) > 0 {
		return result, errors.New("summary not delivered everywhere")
	}
	return result, nil
}
