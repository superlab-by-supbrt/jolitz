package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// Claims say who works on a host; the handover is what the next
// person reads first.

func (s *server) claims(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	cs, err := s.cfg.Store.Claims()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *server) putClaim(w http.ResponseWriter, r *http.Request, u *store.User) {
	host := r.PathValue("host")
	if !parse.ValidHost(host) {
		fail(w, http.StatusBadRequest, "host names are [A-Za-z0-9][A-Za-z0-9.-]*")
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 && !readJSON(w, r, &in) {
		return
	}
	if len(in.Note) > 200 || strings.ContainsAny(in.Note, "\n\r") {
		fail(w, http.StatusBadRequest, "note: one line, up to 200 characters")
		return
	}
	c, err := s.cfg.Store.SetClaim(host, u.Name, in.Note, false)
	if errors.Is(err, store.ErrConflict) {
		held, herr := s.cfg.Store.Claim(host)
		if herr != nil {
			internal(w, herr)
			return
		}
		if u.Role != store.RoleAdmin {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      "the host is claimed by " + held.User + "; only they or an admin release it",
				"claimed_by": held.User, "since": held.Since, "note": held.Note,
			})
			return
		}
		log.Printf("api: admin %s takes over the claim on %s from %s", u.Name, host, held.User)
		c, err = s.cfg.Store.SetClaim(host, u.Name, in.Note, true)
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) deleteClaim(w http.ResponseWriter, r *http.Request, u *store.User) {
	host := r.PathValue("host")
	c, err := s.cfg.Store.Claim(host)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no claim on that host")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if c.User != u.Name && u.Role != store.RoleAdmin {
		fail(w, http.StatusForbidden, "the claim belongs to "+c.User+"; only they or an admin release it")
		return
	}
	if err := s.cfg.Store.DeleteClaim(host); err != nil && !errors.Is(err, store.ErrNotFound) {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handover is GET hosts/{name}/handover: claim, sessions, open
// to-dos, running jobs (to-do lines naming ~/.jolitz/jobs/) and
// the last three changes with their rollback.
func (s *server) handover(w http.ResponseWriter, r *http.Request, _ *store.User) {
	name := r.PathValue("name")
	if !parse.ValidHost(name) {
		fail(w, http.StatusBadRequest, "host names are [A-Za-z0-9][A-Za-z0-9.-]*")
		return
	}
	h, err := s.cfg.Store.Handover(name)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such host")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}
