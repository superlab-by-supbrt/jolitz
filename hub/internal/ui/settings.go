package ui

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/jobs"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/notify"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// ---- settings (admin) --------------------------------------

type channelRow struct {
	Name    string
	Target  string
	Enabled bool
}

type settingsPage struct {
	base
	ConfigPath    string
	Config        []config.KV
	MetricsPublic bool
	Jobs          []jobs.Description
	Channels      []channelRow
	Flash         string
	FlashOK       bool
}

// settings returns the configuration, the defaults when none was
// wired.
func (s *server) settings() *config.Config {
	if s.cfg.Settings != nil {
		return s.cfg.Settings
	}
	return config.Default()
}

// channels lists the three channels with what the page may show
// of them: a server and topic, a server and room, the curlrc
// path. Never a file's content.
func channels(c *config.Config) []channelRow {
	n, m, w := c.Notify.Ntfy, c.Notify.Matrix, c.Notify.Webhook
	rows := []channelRow{{Name: "ntfy", Enabled: n.Enabled()}, {Name: "matrix", Enabled: m.Enabled()}, {Name: "webhook", Enabled: w.Enabled()}}
	if n.Enabled() {
		rows[0].Target = n.Server + "/" + n.Topic
	}
	if m.Enabled() {
		rows[1].Target = m.Server + " " + m.Room
	}
	if w.Enabled() {
		rows[2].Target = w.CurlrcFile
	}
	return rows
}

func (s *server) renderSettings(w http.ResponseWriter, r *http.Request, u *store.User, code int, flash string, ok bool) {
	b, err := s.newBase(u, "Settings", "settings")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c := s.settings()
	p := settingsPage{base: b, ConfigPath: c.Path, Config: c.Public(), MetricsPublic: c.MetricsPublic, Channels: channels(c), Flash: flash, FlashOK: ok}
	if s.cfg.Scheduler != nil {
		p.Jobs = s.cfg.Scheduler.Describe()
	}
	s.render(w, code, "settings", p)
}

func (s *server) settingsPage(w http.ResponseWriter, r *http.Request, u *store.User) {
	if u.Role != store.RoleAdmin {
		s.errorPage(w, r, http.StatusForbidden, "Settings need the admin role.")
		return
	}
	s.renderSettings(w, r, u, http.StatusOK, "", false)
}

// settingsTest is the "send test message" button: one attempt
// through one channel, the outcome on the page.
func (s *server) settingsTest(w http.ResponseWriter, r *http.Request, u *store.User) {
	if u.Role != store.RoleAdmin {
		s.errorPage(w, r, http.StatusForbidden, "Settings need the admin role.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = r.ParseForm()
	channel := strings.TrimSpace(r.PostForm.Get("channel"))
	if !api.IsChannel(channel) {
		s.errorPage(w, r, http.StatusBadRequest, "channel is ntfy, matrix or webhook.")
		return
	}
	senders := notify.FromConfig(s.settings().Notify)
	log.Printf("ui: %s sends a test message via %s", u.Name, channel)
	err := api.SendTest(r.Context(), senders, channel, u.Name, s.cfg.Store.Now())
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderSettings(w, r, u, http.StatusNotFound, channel+" is not configured in hub.toml.", false)
	case err != nil:
		s.renderSettings(w, r, u, http.StatusBadGateway, "test message via "+channel+" failed: "+err.Error(), false)
	default:
		s.renderSettings(w, r, u, http.StatusOK, "test message sent via "+channel+".", true)
	}
}
