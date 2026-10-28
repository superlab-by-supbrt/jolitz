// Package ui renders the hub's pages: server-side html/template,
// one inline stylesheet, one inline script, no external asset.
// Every value from memory passes through the template engine's
// escaping; the only trusted blob is the fixed CSS.
package ui

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/config"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/jobs"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

//go:embed templates/*.html static/hub.css
var files embed.FS

// Config wires the UI. Settings is hub.toml (nil: defaults),
// Scheduler the running job scheduler (nil: none), both for the
// admin's settings page.
type Config struct {
	Store     *store.Store
	Version   string
	BaseURL   string
	Settings  *config.Config
	Scheduler *jobs.Scheduler
}

type server struct {
	cfg   Config
	css   template.CSS
	pages map[string]*template.Template
}

// New returns the handler for every path outside /api/v1/.
func New(cfg Config) http.Handler {
	css, err := files.ReadFile("static/hub.css")
	if err != nil {
		panic(err)
	}
	s := &server{cfg: cfg, css: template.CSS(css), pages: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"agecls":     ageClass,
		"agetext":    ageText,
		"since":      s.since,
		"when":       func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") },
		"join":       strings.Join,
		"index":      func(m map[string]string, k string) string { return m[k] },
		"hasPrefix":  strings.HasPrefix,
		"add":        func(a, b int) int { return a + b },
		"f1":         func(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) },
		"url":        url.PathEscape,
		"ci":         changeItem,
		"add_f":      func(a float64, b int) float64 { return a + float64(b) },
		"index_bool": func(m map[string]bool, k string) bool { return m[k] },
	}
	base := template.Must(template.New("base").Funcs(funcs).ParseFS(files, "templates/base.html"))
	for _, name := range []string{"login", "fleet", "host", "timeline", "activity", "presence", "audit", "guard", "apidoc", "settings", "error"} {
		t := template.Must(template.Must(base.Clone()).ParseFS(files, "templates/"+name+".html"))
		s.pages[name] = t
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.page(s.fleet))
	mux.HandleFunc("GET /hosts/{name}", s.page(s.host))
	mux.HandleFunc("POST /hosts/{name}/claim", s.page(s.claim))
	mux.HandleFunc("POST /hosts/{name}/release", s.page(s.release))
	mux.HandleFunc("POST /hosts/{name}/todos", s.page(s.todos))
	mux.HandleFunc("POST /hosts/{name}/facts", s.page(s.facts))
	mux.HandleFunc("POST /links", s.page(s.links))
	mux.HandleFunc("GET /timeline", s.page(s.timeline))
	mux.HandleFunc("GET /activity", s.page(s.activity))
	mux.HandleFunc("GET /presence", s.page(s.presence))
	mux.HandleFunc("GET /audit", s.page(s.audit))
	mux.HandleFunc("GET /guard", s.page(s.guard))
	mux.HandleFunc("GET /api", s.page(s.apidoc))
	mux.HandleFunc("GET /settings", s.page(s.settingsPage))
	mux.HandleFunc("POST /settings/test", s.page(s.settingsTest))
	mux.HandleFunc("GET /export", s.page(s.export))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.errorPage(w, r, http.StatusNotFound, "No such page.")
	})
	return mux
}

// ---- page plumbing -----------------------------------------

// base is what every page shows: the top bar, the user, the
// footer.
type base struct {
	Title   string
	Nav     string
	User    *store.User
	Version string
	BaseURL string
	Hosts   int
	Online  int
	CSS     template.CSS
	Poll    bool
	Now     string
	CanEdit bool
	Err     string
}

type handler func(w http.ResponseWriter, r *http.Request, u *store.User)

// page requires a login cookie and sends the browser to /login
// without one.
func (s *server) page(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := api.Authenticate(s.cfg.Store, r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r, u)
	}
}

func (s *server) newBase(u *store.User, title, nav string) (base, error) {
	b := base{Title: title, Nav: nav, User: u, Version: s.cfg.Version, BaseURL: s.cfg.BaseURL, CSS: s.css,
		Now: s.cfg.Store.Now().UTC().Format("2006-01-02 15:04 UTC")}
	if u != nil {
		b.CanEdit = store.RoleAtLeast(u.Role, store.RoleOperator)
		hosts, err := s.cfg.Store.Hosts()
		if err != nil {
			return b, err
		}
		b.Hosts = len(hosts)
		sessions, err := s.cfg.Store.OpenSessions()
		if err != nil {
			return b, err
		}
		seen := map[string]bool{}
		for _, se := range sessions {
			if !se.Stale {
				seen[se.User] = true
			}
		}
		b.Online = len(seen)
	}
	return b, nil
}

func (s *server) render(w http.ResponseWriter, code int, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "page", data); err != nil {
		log.Printf("ui: render %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

type errorPage struct {
	base
	Code    int
	Message string
}

func (s *server) errorPage(w http.ResponseWriter, r *http.Request, code int, msg string) {
	u, _ := api.Authenticate(s.cfg.Store, r)
	b, _ := s.newBase(u, fmt.Sprintf("%d", code), "")
	s.render(w, code, "error", errorPage{base: b, Code: code, Message: msg})
}

func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("ui: %s %s: %v", r.Method, r.URL.Path, err)
	s.errorPage(w, r, http.StatusInternalServerError, "Internal error; see the server log.")
}

// ---- helpers for templates ---------------------------------

// ageClass is the pill class of a last-connected age: "" under
// 30 days, warn under 90, bad from 90.
func ageClass(days int) string {
	switch {
	case days < 0:
		return ""
	case days >= 90:
		return "bad"
	case days >= 30:
		return "warn"
	}
	return ""
}

// ageText renders "2026-10-28 (today)", "… (3 days)" or the raw
// value when it is no date.
func ageText(h store.Host) string {
	if h.LastConnectedDays < 0 {
		if h.LastConnected == "" {
			return "unknown"
		}
		return h.LastConnected
	}
	switch h.LastConnectedDays {
	case 0:
		return h.LastConnected + " (today)"
	case 1:
		return h.LastConnected + " (1 day)"
	}
	return fmt.Sprintf("%s (%d days)", h.LastConnected, h.LastConnectedDays)
}

// since is a short relative time: 45 s, 3 min, 2 h, 5 d.
func (s *server) since(t time.Time) string {
	d := s.cfg.Store.Now().Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}

// ---- login -------------------------------------------------

type loginPage struct {
	base
	Next string
}

func (s *server) loginForm(w http.ResponseWriter, r *http.Request) {
	if u, _ := api.Authenticate(s.cfg.Store, r); u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	b, _ := s.newBase(nil, "Login", "")
	s.render(w, http.StatusOK, "login", loginPage{base: b})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "Bad form.")
		return
	}
	token := strings.TrimSpace(r.PostForm.Get("token"))
	u, err := s.cfg.Store.UserByToken(token)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if u == nil {
		b, _ := s.newBase(nil, "Login", "")
		b.Err = "That token is not known here."
		s.render(w, http.StatusUnauthorized, "login", loginPage{base: b})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: api.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: isTLS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: api.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func isTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ---- fleet -------------------------------------------------

type fleetPage struct {
	base
	HostList []store.Host
	Tags     []string
	TagsOf   map[string]string
	Sessions []store.Session
	Claims   []store.Claim
	Todos    []store.TodoRow
	Changes  []parse.Change
	Links    []parse.Link
	Stale    int
	OpenTodo int
}

func tagsOf(hosts []store.Host) map[string]string {
	m := map[string]string{}
	for _, h := range hosts {
		m[h.Name] = strings.Join(h.Tags, " ")
	}
	return m
}

func (s *server) fleet(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Fleet", "fleet")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b.Poll = true
	p := fleetPage{base: b}
	st := s.cfg.Store
	if p.HostList, err = st.Hosts(); err == nil {
		if p.Tags, err = st.Tags(); err == nil {
			if p.Sessions, err = st.OpenSessions(); err == nil {
				if p.Claims, err = st.Claims(); err == nil {
					if p.Todos, err = st.Todos(""); err == nil {
						if p.Changes, err = st.Changes(store.ChangeFilter{Limit: 30}); err == nil {
							p.Links, err = st.Links()
						}
					}
				}
			}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p.TagsOf = tagsOf(p.HostList)
	open := p.Todos[:0]
	for _, t := range p.Todos {
		if t.Open {
			open = append(open, t)
		}
	}
	p.Todos = open
	for _, h := range p.HostList {
		if ageClass(h.LastConnectedDays) == "bad" {
			p.Stale++
		}
		p.OpenTodo += h.Todos
	}
	s.render(w, http.StatusOK, "fleet", p)
}

// ---- host --------------------------------------------------

type hostPage struct {
	base
	H          *store.HostDetail
	Handover   *store.Handover
	Flags      []parse.Change
	Mine       bool
	Note       string
	AuditDrift map[string]bool
	TagsText   string
	// MemoryVersion is the file version of memory.md, 0 without
	// one.
	MemoryVersion int
}

func (s *server) hostName(w http.ResponseWriter, r *http.Request) string {
	name := r.PathValue("name")
	if !parse.ValidHost(name) {
		s.errorPage(w, r, http.StatusBadRequest, "Host names are letters, digits, dots and dashes.")
		return ""
	}
	return name
}

func (s *server) host(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := s.hostName(w, r)
	if name == "" {
		return
	}
	h, err := s.cfg.Store.Host(name)
	if errors.Is(err, store.ErrNotFound) {
		s.errorPage(w, r, http.StatusNotFound, "No such host in the hub.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b, err := s.newBase(u, name, "fleet")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := hostPage{base: b, H: h, AuditDrift: map[string]bool{}, TagsText: strings.Join(h.Tags, ", ")}
	if p.Handover, err = s.cfg.Store.Handover(name); err != nil {
		s.fail(w, r, err)
		return
	}
	if _, info, err := s.cfg.Store.GetFile("servers/" + name + "/memory.md"); err == nil {
		p.MemoryVersion = info.Version
	}
	for _, c := range h.Changes {
		if c.Flags != "" {
			p.Flags = append(p.Flags, c)
		}
	}
	if h.Claim != nil {
		p.Mine = h.Claim.User == u.Name
	}
	if snaps, err := s.cfg.Store.Snapshots(); err == nil && len(snaps) > 0 {
		if vals, err := s.cfg.Store.AuditValues(snaps[0], ""); err == nil {
			for _, k := range driftKeys(vals) {
				p.AuditDrift[k] = true
			}
		}
	}
	s.render(w, http.StatusOK, "host", p)
}

func (s *server) claim(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := s.hostName(w, r)
	if name == "" {
		return
	}
	if !store.RoleAtLeast(u.Role, store.RoleOperator) {
		s.errorPage(w, r, http.StatusForbidden, "Claims need the operator role.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	_ = r.ParseForm()
	note := strings.TrimSpace(r.PostForm.Get("note"))
	if len(note) > 200 {
		note = note[:200]
	}
	_, err := s.cfg.Store.SetClaim(name, u.Name, note, false)
	if errors.Is(err, store.ErrConflict) {
		held, herr := s.cfg.Store.Claim(name)
		if herr != nil {
			s.fail(w, r, herr)
			return
		}
		if u.Role != store.RoleAdmin {
			s.errorPage(w, r, http.StatusConflict, "The host is claimed by "+held.User+"; only they or an admin release it.")
			return
		}
		log.Printf("ui: admin %s takes over the claim on %s from %s", u.Name, name, held.User)
		_, err = s.cfg.Store.SetClaim(name, u.Name, note, true)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/hosts/"+url.PathEscape(name), http.StatusSeeOther)
}

func (s *server) release(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := s.hostName(w, r)
	if name == "" {
		return
	}
	c, err := s.cfg.Store.Claim(name)
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/hosts/"+url.PathEscape(name), http.StatusSeeOther)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !store.RoleAtLeast(u.Role, store.RoleOperator) || (c.User != u.Name && u.Role != store.RoleAdmin) {
		s.errorPage(w, r, http.StatusForbidden, "The claim belongs to "+c.User+"; only they or an admin release it.")
		return
	}
	if err := s.cfg.Store.DeleteClaim(name); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/hosts/"+url.PathEscape(name), http.StatusSeeOther)
}

// ---- timeline ----------------------------------------------

type tlTick struct {
	X      float64
	Label  string
	Anchor string
}

type tlHost struct {
	Name string
	Y    float64
}

type tlDot struct {
	X, Y  float64
	Flag  bool
	Title string
	Host  string
}

type timelinePage struct {
	base
	Days    int
	Windows []int
	W, H    int
	Left    int
	Right   int
	Top     int
	Ticks   []tlTick
	Lanes   []tlHost
	Dots    []tlDot
	Changes []parse.Change
	TagsOf  map[string]string
}

func daysParam(r *http.Request, def int) int {
	d, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || d <= 0 {
		return def
	}
	if d > 36500 {
		d = 36500
	}
	return d
}

func (s *server) timeline(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Timeline", "timeline")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	days := daysParam(r, 30)
	now := s.cfg.Store.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -days)
	hosts, err := s.cfg.Store.Hosts()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	changes, err := s.cfg.Store.Changes(store.ChangeFilter{Since: start.Format("2006-01-02")})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := timelinePage{base: b, Days: days, Windows: []int{7, 30, 90, 365}, W: 960, Left: 210, Right: 20, Top: 30,
		Changes: changes, TagsOf: tagsOf(hosts)}
	lane := 26
	n := len(hosts)
	if n == 0 {
		n = 1
	}
	p.H = p.Top + lane*n + 20
	span := float64(p.W - p.Left - p.Right)
	for t := 0; t <= 4; t++ {
		d := start.AddDate(0, 0, days*t/4)
		anchor := "middle"
		if t == 0 {
			anchor = "start"
		} else if t == 4 {
			anchor = "end"
		}
		p.Ticks = append(p.Ticks, tlTick{X: float64(p.Left) + span*float64(t)/4, Label: d.Format("2006-01-02"), Anchor: anchor})
	}
	yOf := map[string]float64{}
	for i, h := range hosts {
		y := float64(p.Top + lane*i + lane/2)
		yOf[h.Name] = y
		p.Lanes = append(p.Lanes, tlHost{Name: h.Name, Y: y})
	}
	for _, c := range changes {
		y, ok := yOf[c.Host]
		if !ok {
			continue
		}
		d, err := time.Parse("2006-01-02", c.Date)
		if err != nil {
			continue
		}
		if d.After(today) {
			d = today
		}
		x := float64(p.Left) + span*float64(int(d.Sub(start).Hours()/24))/float64(days)
		p.Dots = append(p.Dots, tlDot{X: x, Y: y, Flag: c.Flags != "", Host: c.Host,
			Title: c.Date + " " + c.Host + ": " + c.Headline})
	}
	s.render(w, http.StatusOK, "timeline", p)
}

// ---- activity ----------------------------------------------

type activityPage struct {
	base
	Q       string
	Host    string
	Changes []parse.Change
	Limit   int
	TagsOf  map[string]string
}

func (s *server) activity(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Activity", "activity")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	host := q.Get("host")
	if host != "" && !parse.ValidHost(host) {
		host = ""
	}
	p := activityPage{base: b, Q: q.Get("q"), Host: host, Limit: 200}
	if p.Changes, err = s.cfg.Store.Changes(store.ChangeFilter{Host: host, Q: p.Q, Limit: p.Limit}); err != nil {
		s.fail(w, r, err)
		return
	}
	hosts, err := s.cfg.Store.Hosts()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p.TagsOf = tagsOf(hosts)
	s.render(w, http.StatusOK, "activity", p)
}

// ---- presence ----------------------------------------------

type presencePage struct {
	base
	Sessions []store.Session
	Claims   []store.Claim
}

func (s *server) presence(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Presence", "presence")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b.Poll = true
	p := presencePage{base: b}
	if p.Sessions, err = s.cfg.Store.OpenSessions(); err == nil {
		p.Claims, err = s.cfg.Store.Claims()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "presence", p)
}

// ---- audit -------------------------------------------------

type auditKey struct {
	Key    string
	Drift  bool
	Values []string
}

type auditChange struct {
	Host, Key, From, To string
}

type auditPage struct {
	base
	Date       string
	PrevDate   string
	AuditHosts []string
	Keys       []auditKey
	Changed    []auditChange
	Drift      int
	Snaps      []string
}

// driftKeys names the settings whose readable values disagree
// between hosts.
func driftKeys(vals []parse.AuditValue) []string {
	seen := map[string]map[string]bool{}
	for _, v := range vals {
		if strings.HasPrefix(v.Value, "unknown") {
			continue
		}
		if seen[v.Key] == nil {
			seen[v.Key] = map[string]bool{}
		}
		seen[v.Key][v.Value] = true
	}
	var out []string
	for k, vs := range seen {
		if len(vs) > 1 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (s *server) audit(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Audit", "audit")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := auditPage{base: b}
	if p.Snaps, err = s.cfg.Store.Snapshots(); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(p.Snaps) > 0 {
		cur, err := s.cfg.Store.AuditValues(p.Snaps[0], "")
		if err != nil {
			s.fail(w, r, err)
			return
		}
		p.Date = store.SnapshotDate(p.Snaps[0])
		hostSet := map[string]bool{}
		keyOrder := []string{}
		keySet := map[string]bool{}
		val := map[[2]string]string{}
		for _, v := range cur {
			if !hostSet[v.Host] {
				hostSet[v.Host] = true
				p.AuditHosts = append(p.AuditHosts, v.Host)
			}
			if !keySet[v.Key] {
				keySet[v.Key] = true
				keyOrder = append(keyOrder, v.Key)
			}
			val[[2]string{v.Host, v.Key}] = v.Value
		}
		sort.Strings(p.AuditHosts)
		sort.Strings(keyOrder)
		drift := map[string]bool{}
		for _, k := range driftKeys(cur) {
			drift[k] = true
		}
		p.Drift = len(drift)
		for _, k := range keyOrder {
			ak := auditKey{Key: k, Drift: drift[k]}
			for _, h := range p.AuditHosts {
				ak.Values = append(ak.Values, val[[2]string{h, k}])
			}
			p.Keys = append(p.Keys, ak)
		}
		if len(p.Snaps) > 1 {
			prev, err := s.cfg.Store.AuditValues(p.Snaps[1], "")
			if err != nil {
				s.fail(w, r, err)
				return
			}
			p.PrevDate = store.SnapshotDate(p.Snaps[1])
			for _, v := range prev {
				if now, ok := val[[2]string{v.Host, v.Key}]; ok && now != v.Value {
					p.Changed = append(p.Changed, auditChange{Host: v.Host, Key: v.Key, From: v.Value, To: now})
				}
			}
		}
	}
	s.render(w, http.StatusOK, "audit", p)
}

// ---- guard -------------------------------------------------

type guardPage struct {
	base
	Since  string
	Counts []store.GuardCount
	Total  int
}

func (s *server) guard(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "Guard", "guard")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	since, err := api.SinceParam(r.URL.Query().Get("since"))
	if err != nil || since.IsZero() {
		since = s.cfg.Store.Now().Add(-30 * 24 * time.Hour)
	}
	p := guardPage{base: b, Since: since.UTC().Format("2006-01-02")}
	if p.Counts, err = s.cfg.Store.GuardCounts(since); err != nil {
		s.fail(w, r, err)
		return
	}
	for _, c := range p.Counts {
		p.Total += c.Count
	}
	s.render(w, http.StatusOK, "guard", p)
}

// ---- api doc -----------------------------------------------

type apiPage struct {
	base
	Endpoints []api.Endpoint
	Hub       string
}

func (s *server) apidoc(w http.ResponseWriter, r *http.Request, u *store.User) {
	b, err := s.newBase(u, "API", "api")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	hub := strings.TrimRight(s.cfg.BaseURL, "/")
	if hub == "" {
		hub = "http://" + r.Host
	}
	s.render(w, http.StatusOK, "apidoc", apiPage{base: b, Endpoints: api.Endpoints, Hub: hub})
}

// ---- export ------------------------------------------------

func (s *server) export(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var buf bytes.Buffer
	if err := api.Export(s.cfg.Store, &buf); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="jolitz-memory-%s.tar"`, s.cfg.Store.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// changeView is one changelog entry for the "change" partial.
type changeView struct {
	C    parse.Change
	Tags string
}

func changeItem(c parse.Change, tags map[string]string) changeView {
	return changeView{C: c, Tags: tags[c.Host]}
}
