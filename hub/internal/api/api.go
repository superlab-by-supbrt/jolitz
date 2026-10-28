// Package api is the HTTP interface of the hub under /api/v1/.
// JSON in and out unless an endpoint says otherwise; a bearer
// token per request (or the login cookie the UI sets). Roles:
// viewer reads, operator writes files, sessions, claims and guard
// denials, admin also releases other people's claims and ends
// their sessions. Users are made on the command line, not here.
package api

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/lint"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// CookieName is the login cookie of the UI; it holds the same
// token a client sends as Bearer.
const CookieName = "jolitz_hub"

// MaxBody bounds a request body.
const MaxBody = 16 << 20

// Config wires the API. Settings is hub.toml; nil means the
// defaults.
type Config struct {
	Store    *store.Store
	Version  string
	BaseURL  string
	Settings *config.Config
}

// Endpoint documents one route; the /api page renders the list.
type Endpoint struct {
	Method  string
	Path    string
	Role    string
	Desc    string
	Example string
}

// Endpoints is the documented interface, in the order of the
// plan. Paths are relative to /api/v1/.
var Endpoints = []Endpoint{
	{"GET", "health", "", "Liveness, no token needed.", `curl -sS $HUB/api/v1/health`},
	{"GET", "whoami", "viewer", "Name and role of the token's user.", `curl -sS -H @"$T" $HUB/api/v1/whoami`},
	{"GET", "files", "viewer", "Manifest: path, sha256, size, updated_at, updated_by of every file.", `curl -sS -H @"$T" $HUB/api/v1/files`},
	{"GET", "files/{path}", "viewer", "Raw bytes of one file; ETag is its sha256.", `curl -sS -H @"$T" $HUB/api/v1/files/servers/web1.example.com/memory.md`},
	{"PUT", "files/{path}", "operator", "Store a file (raw body). If-Match: <sha256> gives 412 on a mismatch; a value that looks like a secret gives 422 {line, reason}; a path outside the allowed set 400.", `curl -sS -H @"$T" -X PUT --data-binary @memory/servers/web1.example.com/memory.md -H 'If-Match: <sha256>' $HUB/api/v1/files/servers/web1.example.com/memory.md`},
	{"DELETE", "files/{path}", "operator", "Remove a file and what was derived from it.", `curl -sS -H @"$T" -X DELETE $HUB/api/v1/files/servers/old.example.com/todo.md`},
	{"GET", "export", "viewer", "Every file as a tar archive; unpack into memory/.", `curl -sS -H @"$T" $HUB/api/v1/export | tar -x -C memory/`},
	{"GET", "hosts", "viewer", "The fleet, parsed: the fields of overview.json plus claim and sessions.", `curl -sS -H @"$T" $HUB/api/v1/hosts`},
	{"GET", "hosts/{name}", "viewer", "One host with changes, to-dos, rules.md, memory.md and audit values.", `curl -sS -H @"$T" $HUB/api/v1/hosts/web1.example.com`},
	{"GET", "changes", "viewer", "Changelog entries, newest first. ?host= ?since=YYYY-MM-DD ?q= ?limit=", `curl -sS -H @"$T" "$HUB/api/v1/changes?host=web1.example.com&limit=20"`},
	{"GET", "todos", "viewer", "To-do items. ?host=", `curl -sS -H @"$T" "$HUB/api/v1/todos?host=db1.example.com"`},
	{"GET", "audits", "viewer", "Fleet audit values of every snapshot. ?host=", `curl -sS -H @"$T" $HUB/api/v1/audits`},
	{"GET", "links", "viewer", "The - Link: lines of network.md.", `curl -sS -H @"$T" $HUB/api/v1/links`},
	{"POST", "sessions", "operator", "Open a session {host, workstation}; returns {id}.", `curl -sS -H @"$T" -d '{"host":"web1.example.com","workstation":"alices-mbp"}' $HUB/api/v1/sessions`},
	{"POST", "sessions/{id}/heartbeat", "operator", "Keep a session alive; stale after 10 minutes, closed after 60.", `curl -sS -H @"$T" -X POST $HUB/api/v1/sessions/$ID/heartbeat`},
	{"DELETE", "sessions/{id}", "operator", "End a session (own, or admin).", `curl -sS -H @"$T" -X DELETE $HUB/api/v1/sessions/$ID`},
	{"GET", "sessions", "viewer", "Open sessions with age and stale flag.", `curl -sS -H @"$T" $HUB/api/v1/sessions`},
	{"GET", "claims", "viewer", "Who holds which host, since when, and why.", `curl -sS -H @"$T" $HUB/api/v1/claims`},
	{"PUT", "claims/{host}", "operator", "Claim a host for yourself {note}; 409 {claimed_by, since} when someone else holds it (an admin takes over).", `curl -sS -H @"$T" -X PUT -d '{"note":"migration until 20:00"}' $HUB/api/v1/claims/db1.example.com`},
	{"DELETE", "claims/{host}", "operator", "Release a host (own claim, or admin).", `curl -sS -H @"$T" -X DELETE $HUB/api/v1/claims/db1.example.com`},
	{"POST", "guard", "operator", "Record a guard denial {time, reason}. A body with a command key is refused (400): the hub never holds command text.", `curl -sS -H @"$T" -d '{"time":"2026-10-28T10:00:00Z","reason":"powering off a machine"}' $HUB/api/v1/guard`},
	{"GET", "guard", "viewer", "Denials since a date, counted by reason. ?since=YYYY-MM-DD (default 30 days)", `curl -sS -H @"$T" $HUB/api/v1/guard`},
	{"GET", "overview.json", "viewer", "The fleet as bin/jolitz-overview --json writes it: hosts with eol and audit, todos, changes, network, audit, guard, lint (always empty here).", `curl -sS -H @"$T" $HUB/api/v1/overview.json`},
	{"POST", "notify/test", "admin", "Send one test message through a configured channel {channel: ntfy|matrix|webhook}; 404 when it is not configured, 502 when the send fails.", `curl -sS -H @"$T" -d '{"channel":"ntfy"}' $HUB/api/v1/notify/test`},
}

// MetricsPath is where the Prometheus gauges live, outside
// /api/v1/ as scrapers expect.
const MetricsPath = "/metrics"

type server struct {
	cfg Config
}

// New returns the handler for /api/v1/.
func New(cfg Config) http.Handler {
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	p := "/api/v1/"
	mux.HandleFunc("GET "+p+"health", s.health)
	mux.HandleFunc("GET "+p+"whoami", s.auth(store.RoleViewer, s.whoami))
	mux.HandleFunc("GET "+p+"files", s.auth(store.RoleViewer, s.listFiles))
	mux.HandleFunc("GET "+p+"files/{path...}", s.auth(store.RoleViewer, s.getFile))
	mux.HandleFunc("PUT "+p+"files/{path...}", s.auth(store.RoleOperator, s.putFile))
	mux.HandleFunc("DELETE "+p+"files/{path...}", s.auth(store.RoleOperator, s.deleteFile))
	mux.HandleFunc("GET "+p+"export", s.auth(store.RoleViewer, s.export))
	mux.HandleFunc("GET "+p+"hosts", s.auth(store.RoleViewer, s.hosts))
	mux.HandleFunc("GET "+p+"hosts/{name}", s.auth(store.RoleViewer, s.host))
	mux.HandleFunc("GET "+p+"changes", s.auth(store.RoleViewer, s.changes))
	mux.HandleFunc("GET "+p+"todos", s.auth(store.RoleViewer, s.todos))
	mux.HandleFunc("GET "+p+"audits", s.auth(store.RoleViewer, s.audits))
	mux.HandleFunc("GET "+p+"links", s.auth(store.RoleViewer, s.links))
	mux.HandleFunc("POST "+p+"sessions", s.auth(store.RoleOperator, s.startSession))
	mux.HandleFunc("POST "+p+"sessions/{id}/heartbeat", s.auth(store.RoleOperator, s.heartbeat))
	mux.HandleFunc("DELETE "+p+"sessions/{id}", s.auth(store.RoleOperator, s.endSession))
	mux.HandleFunc("GET "+p+"sessions", s.auth(store.RoleViewer, s.sessions))
	mux.HandleFunc("GET "+p+"claims", s.auth(store.RoleViewer, s.claims))
	mux.HandleFunc("PUT "+p+"claims/{host}", s.auth(store.RoleOperator, s.putClaim))
	mux.HandleFunc("DELETE "+p+"claims/{host}", s.auth(store.RoleOperator, s.deleteClaim))
	mux.HandleFunc("POST "+p+"guard", s.auth(store.RoleOperator, s.postGuard))
	mux.HandleFunc("GET "+p+"guard", s.auth(store.RoleViewer, s.guard))
	mux.HandleFunc("GET "+p+"overview.json", s.auth(store.RoleViewer, s.overviewJSON))
	mux.HandleFunc("POST "+p+"notify/test", s.auth(store.RoleAdmin, s.notifyTest))
	mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "no such endpoint; GET /api lists them")
	})
	return mux
}

// ---- auth --------------------------------------------------

// TokenFromRequest reads the bearer token, or the login cookie.
func TokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return ""
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// Authenticate resolves the request's user; nil without a valid
// token.
func Authenticate(st *store.Store, r *http.Request) (*store.User, error) {
	return st.UserByToken(TokenFromRequest(r))
}

type handler func(w http.ResponseWriter, r *http.Request, u *store.User)

func (s *server) auth(min string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := Authenticate(s.cfg.Store, r)
		if err != nil {
			internal(w, err)
			return
		}
		if u == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jolitz hub"`)
			fail(w, http.StatusUnauthorized, "a valid token is required")
			return
		}
		if !store.RoleAtLeast(u.Role, min) {
			fail(w, http.StatusForbidden, "this needs the "+min+" role")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		h(w, r, u)
	}
}

// ---- helpers -----------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func internal(w http.ResponseWriter, err error) {
	log.Printf("api: %v", err)
	fail(w, http.StatusInternalServerError, "internal error")
}

// readJSON decodes a small JSON object; unknown shapes fail.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "body must be a JSON object: "+err.Error())
		return false
	}
	return true
}

// ---- handlers ----------------------------------------------

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.cfg.Version})
}

func (s *server) whoami(w http.ResponseWriter, _ *http.Request, u *store.User) {
	writeJSON(w, http.StatusOK, map[string]string{"user": u.Name, "role": u.Role})
}

func (s *server) listFiles(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	files, err := s.cfg.Store.ListFiles()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

// filePath validates the path wildcard; "" with a 400 written.
func filePath(w http.ResponseWriter, r *http.Request) string {
	p := r.PathValue("path")
	if kind, _ := store.Classify(p); kind == store.KindInvalid {
		fail(w, http.StatusBadRequest, "path outside the allowed set: servers/<host>/(memory.md|changelog.log|todo.md|rules.md), servers/<alias>, network.md, audits/<YYYY-MM-DD-HHMM>.json; host names are [A-Za-z0-9][A-Za-z0-9.-]*")
		return ""
	}
	return p
}

func (s *server) getFile(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := filePath(w, r)
	if p == "" {
		return
	}
	body, info, err := s.cfg.Store.GetFile(p)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such file")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+info.SHA256+`"`)
	w.Header().Set("Last-Modified", info.UpdatedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *server) putFile(w http.ResponseWriter, r *http.Request, u *store.User) {
	p := filePath(w, r)
	if p == "" {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}
	if hit := lint.Check(body); hit != nil {
		writeJSON(w, http.StatusUnprocessableEntity, hit)
		return
	}
	ifMatch := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `" `)
	if ifMatch == "*" {
		ifMatch = ""
	}
	_, _, getErr := s.cfg.Store.GetFile(p)
	created := errors.Is(getErr, store.ErrNotFound)
	info, err := s.cfg.Store.PutFile(p, body, u.Name, ifMatch)
	switch {
	case errors.Is(err, store.ErrMismatch):
		fail(w, http.StatusPreconditionFailed, "If-Match does not match the stored sha256; pull first")
		return
	case errors.Is(err, store.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		internal(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, info)
}

func (s *server) deleteFile(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := filePath(w, r)
	if p == "" {
		return
	}
	err := s.cfg.Store.DeleteFile(p)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such file")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Export writes every file as a tar archive with paths relative
// to memory/.
func Export(st *store.Store, w io.Writer) error {
	files, err := st.ListFiles()
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	for _, f := range files {
		body, info, err := st.GetFile(f.Path)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: f.Path, Mode: 0o644, Size: int64(len(body)), ModTime: info.UpdatedAt,
		}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	return tw.Close()
}

func (s *server) export(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	var buf bytes.Buffer
	if err := Export(s.cfg.Store, &buf); err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="jolitz-memory-%s.tar"`, s.cfg.Store.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) hosts(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	hosts, err := s.cfg.Store.Hosts()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *server) host(w http.ResponseWriter, r *http.Request, _ *store.User) {
	name := r.PathValue("name")
	if !parse.ValidHost(name) {
		fail(w, http.StatusBadRequest, "host names are [A-Za-z0-9][A-Za-z0-9.-]*")
		return
	}
	h, err := s.cfg.Store.Host(name)
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

func (s *server) changes(w http.ResponseWriter, r *http.Request, _ *store.User) {
	q := r.URL.Query()
	f := store.ChangeFilter{Host: q.Get("host"), Since: q.Get("since"), Q: q.Get("q")}
	if l := q.Get("limit"); l != "" {
		if _, err := fmt.Sscanf(l, "%d", &f.Limit); err != nil || f.Limit < 0 {
			fail(w, http.StatusBadRequest, "limit must be a number")
			return
		}
	}
	cs, err := s.cfg.Store.Changes(f)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *server) todos(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ts, err := s.cfg.Store.Todos(r.URL.Query().Get("host"))
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *server) audits(w http.ResponseWriter, r *http.Request, _ *store.User) {
	rows, err := s.cfg.Store.Audits(r.URL.Query().Get("host"))
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *server) links(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	ls, err := s.cfg.Store.Links()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in struct {
		Host        string `json:"host"`
		Workstation string `json:"workstation"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Host != "" && !parse.ValidHost(in.Host) {
		fail(w, http.StatusBadRequest, "host names are [A-Za-z0-9][A-Za-z0-9.-]*")
		return
	}
	if len(in.Workstation) > 100 || strings.ContainsAny(in.Workstation, "\n\r\t") {
		fail(w, http.StatusBadRequest, "workstation: one line, up to 100 characters")
		return
	}
	id, err := s.cfg.Store.StartSession(u.Name, in.Host, in.Workstation)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) heartbeat(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	se, ended, err := s.cfg.Store.Session(id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && ended) {
		fail(w, http.StatusNotFound, "no open session with that id; start a new one")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if se.User != u.Name && u.Role != store.RoleAdmin {
		fail(w, http.StatusForbidden, "not your session")
		return
	}
	if err := s.cfg.Store.Heartbeat(id); errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "the session was closed; start a new one")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) endSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	se, _, err := s.cfg.Store.Session(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such session")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if se.User != u.Name && u.Role != store.RoleAdmin {
		fail(w, http.StatusForbidden, "not your session")
		return
	}
	if err := s.cfg.Store.EndSession(id); err != nil && !errors.Is(err, store.ErrNotFound) {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) sessions(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	ss, err := s.cfg.Store.OpenSessions()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ss)
}

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

func (s *server) postGuard(w http.ResponseWriter, r *http.Request, u *store.User) {
	var raw map[string]json.RawMessage
	if !readJSON(w, r, &raw) {
		return
	}
	if _, has := raw["command"]; has {
		fail(w, http.StatusBadRequest, "the hub takes the reason of a denial, never the command")
		return
	}
	var reason, when string
	_ = json.Unmarshal(raw["reason"], &reason)
	_ = json.Unmarshal(raw["time"], &when)
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 200 || strings.ContainsAny(reason, "\n\r") {
		fail(w, http.StatusBadRequest, "reason: one line, up to 200 characters")
		return
	}
	at := s.cfg.Store.Now()
	if when != "" {
		t, err := time.Parse(time.RFC3339, when)
		if err != nil {
			fail(w, http.StatusBadRequest, "time must be RFC 3339, e.g. 2026-10-28T10:00:00Z")
			return
		}
		at = t
	}
	if err := s.cfg.Store.AddGuard(u.Name, at, reason); err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// SinceParam reads ?since= as YYYY-MM-DD or RFC 3339; the zero
// time when absent, an error when malformed.
func SinceParam(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func (s *server) guard(w http.ResponseWriter, r *http.Request, _ *store.User) {
	since, err := SinceParam(r.URL.Query().Get("since"))
	if err != nil {
		fail(w, http.StatusBadRequest, "since must be YYYY-MM-DD")
		return
	}
	if since.IsZero() {
		since = s.cfg.Store.Now().Add(-30 * 24 * time.Hour)
	}
	gs, err := s.cfg.Store.GuardCounts(since)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gs)
}
