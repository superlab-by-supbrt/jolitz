package api

import (
	"bytes"
	"net/http"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/overview"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// settings returns the configuration, the defaults when none was
// wired.
func (s *server) settings() *config.Config {
	if s.cfg.Settings != nil {
		return s.cfg.Settings
	}
	return config.Default()
}

// overviewJSON is GET /api/v1/overview.json: the shape of
// bin/jolitz-overview --json, with the 30-day window the HTML
// build uses.
func (s *server) overviewJSON(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	d, err := overview.Load(s.cfg.Store, 30)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d.JSON())
}

// Metrics returns the handler for /metrics: the gauges of
// jolitz.prom plus the hub's own, in the Prometheus text format.
// A token is required unless metrics_public is set, in which case
// the reverse proxy or the firewall decides who reaches it.
func Metrics(cfg Config) http.Handler {
	s := &server{cfg: cfg}
	serve := func(w http.ResponseWriter, _ *http.Request, _ *store.User) {
		d, err := overview.Load(s.cfg.Store, 30)
		if err != nil {
			internal(w, err)
			return
		}
		var buf bytes.Buffer
		d.Prom(&buf)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, http.StatusMethodNotAllowed, "GET only")
			return
		}
		if s.settings().MetricsPublic {
			serve(w, r, nil)
			return
		}
		s.auth(store.RoleViewer, serve)(w, r)
	})
}
