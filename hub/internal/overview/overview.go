// Package overview is the Go port of lib/overview.awk's shared
// computations and of its text(), json_out() and prom_out()
// renderers, fed from the store instead of memory/ files. The
// hub serves the result as the weekly fleet summary, as
// /api/v1/overview.json and as /metrics.
//
// What the awk has and the hub has not: the warnings of
// bin/jolitz-memory-lint. The lint runs on the workstation
// before every push and the hub refuses a file with a hit, so
// there is nothing to list; the text leaves the "Memory lint"
// section out and the JSON carries an empty "lint" array.
package overview

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// Data is everything one rendering needs, read once.
type Data struct {
	Today string // YYYY-MM-DD
	Now   string // YYYY-MM-DD HH:MM
	Days  int    // window of "recent" changes

	Hosts   []store.Host // by name
	Todos   []store.TodoRow
	Changes []parse.Change // newest first
	Links   []parse.Link
	Guard   []store.GuardCount // last 30 days

	AuditDate, PrevDate   string
	auditCur, auditPrev   map[[2]string]string
	auditHosts, auditKeys []string

	eol      map[string]map[string]string // product -> cycle -> eolFrom
	fetched  map[string]string            // product -> YYYY-MM-DD
	products []string

	SessionsOpen int
	Files        int
	LastPush     map[string]time.Time

	today int // day number of Today
	now   time.Time
}

// Load reads the fleet from the store. days is the window for
// "changes in N days" (7 for the text summary, 30 for JSON).
func Load(st *store.Store, days int) (*Data, error) {
	now := st.Now().UTC()
	d := &Data{Days: days, now: now, Today: now.Format("2006-01-02"), Now: now.Format("2006-01-02 15:04"),
		auditCur: map[[2]string]string{}, auditPrev: map[[2]string]string{}, eol: map[string]map[string]string{}, fetched: map[string]string{}}
	d.today = dnum(d.Today)
	var err error
	if d.Hosts, err = st.Hosts(); err != nil {
		return nil, err
	}
	all, err := st.Todos("")
	if err != nil {
		return nil, err
	}
	d.Todos = []store.TodoRow{}
	for _, t := range all {
		if t.Open {
			d.Todos = append(d.Todos, t)
		}
	}
	if d.Changes, err = st.Changes(store.ChangeFilter{}); err != nil {
		return nil, err
	}
	if d.Links, err = st.Links(); err != nil {
		return nil, err
	}
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	if d.Guard, err = st.GuardCounts(since); err != nil {
		return nil, err
	}
	snaps, err := st.Snapshots()
	if err != nil {
		return nil, err
	}
	if len(snaps) > 0 {
		d.AuditDate = store.SnapshotDate(snaps[0])
		vals, err := st.AuditValues(snaps[0], "")
		if err != nil {
			return nil, err
		}
		hs, ks := map[string]bool{}, map[string]bool{}
		for _, v := range vals {
			d.auditCur[[2]string{v.Host, v.Key}] = v.Value
			if !hs[v.Host] {
				hs[v.Host] = true
				d.auditHosts = append(d.auditHosts, v.Host)
			}
			if !ks[v.Key] {
				ks[v.Key] = true
				d.auditKeys = append(d.auditKeys, v.Key)
			}
		}
		sort.Strings(d.auditHosts)
		sort.Strings(d.auditKeys)
	}
	if len(snaps) > 1 {
		d.PrevDate = store.SnapshotDate(snaps[1])
		vals, err := st.AuditValues(snaps[1], "")
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			d.auditPrev[[2]string{v.Host, v.Key}] = v.Value
		}
	}
	entries, err := st.EOL()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		rel := parseEOL(e.Body)
		if rel == nil {
			continue
		}
		d.eol[e.Product] = rel
		d.fetched[e.Product] = e.Fetched
		d.products = append(d.products, e.Product)
	}
	sessions, err := st.OpenSessions()
	if err != nil {
		return nil, err
	}
	d.SessionsOpen = len(sessions)
	if d.Files, err = st.CountFiles(); err != nil {
		return nil, err
	}
	if d.LastPush, err = st.LastPushByUser(); err != nil {
		return nil, err
	}
	return d, nil
}

// ---- dates -------------------------------------------------

// dnum is the day number of a YYYY-MM-DD date (days since
// 1970-01-01), -1 when the string is no such date.
func dnum(s string) int {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return -1
	}
	return int(t.Unix() / 86400)
}

func dstr(n int) string {
	return time.Unix(int64(n)*86400, 0).UTC().Format("2006-01-02")
}

// ageDays is the number of days since a host's last connection,
// -1 when the value is no date.
func (d *Data) ageDays(h store.Host) int {
	a := dnum(h.LastConnected)
	if a < 0 {
		return -1
	}
	return d.today - a
}

// ageClass is "" under 30 days, "old" under 90, "stale" from 90.
func (d *Data) ageClass(h store.Host) string {
	a := d.ageDays(h)
	switch {
	case a < 0:
		return ""
	case a >= 90:
		return "stale"
	case a >= 30:
		return "old"
	}
	return ""
}

func (d *Data) ageText(h store.Host) string {
	a := d.ageDays(h)
	if a < 0 {
		if h.LastConnected == "" {
			return "unknown"
		}
		return h.LastConnected
	}
	switch a {
	case 0:
		return h.LastConnected + " (today)"
	case 1:
		return h.LastConnected + " (1 day)"
	}
	return fmt.Sprintf("%s (%d days)", h.LastConnected, a)
}

// ---- end of life -------------------------------------------

// EOL is the support state of one host's OS release.
type EOL struct {
	State string `json:"state"` // ended|soon|ok|open|unlisted|nodata|""
	Text  string `json:"text"`
	Days  int    `json:"days"` // days left, 0 once ended, -1 unknown
}

// parseEOL reads the answer of
// https://endoflife.date/api/v1/products/<product>/ into
// cycle -> eolFrom; nil when the body is no such answer.
func parseEOL(body []byte) map[string]string {
	var v struct {
		Result struct {
			Releases []struct {
				Name    string `json:"name"`
				EOLFrom string `json:"eolFrom"`
			} `json:"releases"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.Result.Releases == nil {
		return nil
	}
	out := map[string]string{}
	for _, r := range v.Result.Releases {
		out[r.Name] = r.EOLFrom
	}
	return out
}

// eolOf is the awk's eol(n): try the full version, then
// major.minor, then major against the product's cycles.
func eolOf(table map[string]map[string]string, product, version string, today int) EOL {
	e := EOL{Days: -1}
	if product == "" {
		return e
	}
	rel, ok := table[product]
	if !ok {
		e.State, e.Text = "nodata", "no data (eol job pending)"
		return e
	}
	candidates := []string{version}
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		candidates = append(candidates, parts[0]+"."+parts[1])
	}
	candidates = append(candidates, parts[0])
	for _, c := range candidates {
		if c == "" {
			continue
		}
		end, ok := rel[c]
		if !ok {
			continue
		}
		if end == "" {
			e.State, e.Text = "open", "no end date announced"
			return e
		}
		k := dnum(end) - today
		e.Days = k
		if k < 0 {
			e.Days = 0
			e.State, e.Text = "ended", "ended "+dstr(today+k)
		} else if k <= 180 {
			e.State, e.Text = "soon", fmt.Sprintf("ends %s (%d days)", dstr(today+k), k)
		} else {
			e.State, e.Text = "ok", "until "+dstr(today+k)
		}
		return e
	}
	e.State, e.Text = "unlisted", "release "+version+" not listed for "+product
	return e
}

// EOLOf evaluates one host.
func (d *Data) EOLOf(h store.Host) EOL {
	return eolOf(d.eol, h.Product, h.Version, d.today)
}

// ---- shared computations -----------------------------------

func (d *Data) recent(c parse.Change) bool {
	n := dnum(c.Date)
	return n >= 0 && d.today-n <= d.Days
}

func (d *Data) recentCount() int {
	n := 0
	for _, c := range d.Changes {
		if d.recent(c) {
			n++
		}
	}
	return n
}

// driftKeys: settings whose readable values disagree between
// hosts in the newest snapshot.
func (d *Data) driftKeys() []string {
	var out []string
	for _, k := range d.auditKeys {
		seen := map[string]bool{}
		for _, h := range d.auditHosts {
			v, ok := d.auditCur[[2]string{h, k}]
			if !ok || strings.HasPrefix(v, "unknown") {
				continue
			}
			seen[v] = true
		}
		if len(seen) > 1 {
			out = append(out, k)
		}
	}
	return out
}

// AuditChange is one value that differs from the previous
// snapshot.
type AuditChange struct {
	Host string `json:"host"`
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

func (d *Data) auditChanges() []AuditChange {
	out := []AuditChange{}
	for _, h := range d.auditHosts {
		for _, k := range d.auditKeys {
			pv, okp := d.auditPrev[[2]string{h, k}]
			cv, okc := d.auditCur[[2]string{h, k}]
			if okp && okc && pv != cv {
				out = append(out, AuditChange{Host: h, Key: k, From: pv, To: cv})
			}
		}
	}
	return out
}

func (d *Data) guardTotal() int {
	n := 0
	for _, g := range d.Guard {
		n += g.Count
	}
	return n
}

// ---- text --------------------------------------------------

// Text is bin/jolitz-overview --text, section by section.
func (d *Data) Text() string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	p("jolitz fleet summary, %s", d.Today)
	p("Hosts: %d, open to-dos: %d, changes in %d days: %d", len(d.Hosts), len(d.Todos), d.Days, d.recentCount())
	p("")
	p("Changes, last %d days", d.Days)
	any := false
	for _, c := range d.Changes {
		if d.recent(c) {
			p("  %s %s: %s", c.Date, c.Host, c.Headline)
			any = true
		}
	}
	if !any {
		p("  none")
	}
	p("")
	p("Open to-dos")
	for _, t := range d.Todos {
		p("  %s: %s", t.Host, t.Text)
	}
	if len(d.Todos) == 0 {
		p("  none")
	}
	p("")
	p("Not connected for 30 days or more")
	any = false
	for _, h := range d.Hosts {
		if d.ageClass(h) != "" {
			p("  %s: last %s", h.Name, d.ageText(h))
			any = true
		}
	}
	if !any {
		p("  none")
	}
	p("")
	owners := false
	for _, h := range d.Hosts {
		if h.Owner != "" {
			owners = true
		}
	}
	if owners {
		p("Owners")
		for _, h := range d.Hosts {
			if h.Owner != "" {
				p("  %s: %s", h.Name, h.Owner)
			}
		}
		p("")
	}
	p("End of life")
	any = false
	for _, h := range d.Hosts {
		e := d.EOLOf(h)
		if e.State == "ended" || e.State == "soon" {
			p("  %s: %s, %s", h.Name, h.OS, e.Text)
			any = true
		}
	}
	if !any {
		p("  none within 180 days")
	}
	for _, h := range d.Hosts {
		if d.EOLOf(h).State == "nodata" {
			// The awk says "run with --eol"; the hub fetches
			// by itself.
			p("  some systems have no EOL data yet: the eol job fetches it daily")
			break
		}
	}
	if len(d.auditKeys) > 0 {
		p("")
		changes := d.auditChanges()
		line := fmt.Sprintf("Fleet audit %s: %d settings differ", d.AuditDate, len(d.driftKeys()))
		if d.PrevDate != "" {
			line += fmt.Sprintf(", %d changed since %s", len(changes), d.PrevDate)
		}
		p("%s", line)
		for _, c := range changes {
			p("  %s %s: %s -> %s", c.Host, c.Key, c.From, c.To)
		}
	}
	if n := d.guardTotal(); n > 0 {
		p("")
		p("Guard denials, last 30 days: %d", n)
		for _, g := range d.Guard {
			r := g.Reason
			if len(r) > 100 {
				r = r[:100]
			}
			p("  %dx %s", g.Count, r)
		}
	}
	// No "Memory lint" section: the hub holds no lint output.
	if len(d.products) > 0 {
		p("")
		var parts []string
		for _, pr := range d.products {
			parts = append(parts, pr+" "+d.fetched[pr])
		}
		p("EOL dates: endoflife.date, fetched %s", strings.Join(parts, ", "))
	}
	return b.String()
}

// ---- json --------------------------------------------------

// JSONHost is one element of overview.json's hosts.
type JSONHost struct {
	Name              string            `json:"name"`
	OS                string            `json:"os"`
	IP                string            `json:"ip"`
	DistroFamily      string            `json:"distro_family"`
	LastConnected     string            `json:"last_connected"`
	LastConnectedDays int               `json:"last_connected_days"`
	Tags              []string          `json:"tags"`
	SSHPort           string            `json:"ssh_port"`
	Mode              string            `json:"mode"`
	Rules             bool              `json:"rules"`
	Todos             int               `json:"todos"`
	LastChange        string            `json:"last_change"`
	Product           string            `json:"product"`
	Version           string            `json:"version"`
	Owner             string            `json:"owner"`
	Aliases           []string          `json:"aliases"`
	Services          []parse.Service   `json:"services"`
	EOL               EOL               `json:"eol"`
	Audit             map[string]string `json:"audit"`
}

// JSONTodo is one open item.
type JSONTodo struct {
	Host string `json:"host"`
	Text string `json:"text"`
}

// JSONGuardReason is one reason with its count.
type JSONGuardReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Last   string `json:"last"`
}

// JSONOverview is overview.json.
type JSONOverview struct {
	Generated string         `json:"generated"`
	Today     string         `json:"today"`
	Days      int            `json:"days"`
	Tag       string         `json:"tag"`
	Hosts     []JSONHost     `json:"hosts"`
	Todos     []JSONTodo     `json:"todos"`
	Changes   []parse.Change `json:"changes"`
	Network   []parse.Link   `json:"network"`
	Audit     struct {
		Date     string        `json:"date"`
		Previous string        `json:"previous"`
		Drift    int           `json:"drift"`
		Changed  []AuditChange `json:"changed"`
	} `json:"audit"`
	Guard struct {
		Denials30d int               `json:"denials_30d"`
		Reasons    []JSONGuardReason `json:"reasons"`
	} `json:"guard"`
	Lint []string `json:"lint"`
}

// JSON builds overview.json's shape.
func (d *Data) JSON() *JSONOverview {
	o := &JSONOverview{Generated: d.Now, Today: d.Today, Days: d.Days, Hosts: []JSONHost{}, Todos: []JSONTodo{},
		Changes: d.Changes, Network: d.Links, Lint: []string{}}
	for _, h := range d.Hosts {
		o.Hosts = append(o.Hosts, JSONHost{
			Name: h.Name, OS: h.OS, IP: h.IP, DistroFamily: h.DistroFamily, LastConnected: h.LastConnected,
			LastConnectedDays: d.ageDays(h), Tags: h.Tags, SSHPort: h.SSHPort, Mode: h.Mode, Rules: h.Rules,
			Todos: h.Todos, LastChange: h.LastChange, Product: h.Product, Version: h.Version, Owner: h.Owner,
			Aliases: h.Aliases, Services: h.Services, EOL: d.EOLOf(h), Audit: h.Audit,
		})
	}
	for _, t := range d.Todos {
		o.Todos = append(o.Todos, JSONTodo{Host: t.Host, Text: t.Text})
	}
	o.Audit.Date, o.Audit.Previous = d.AuditDate, d.PrevDate
	o.Audit.Drift = len(d.driftKeys())
	o.Audit.Changed = d.auditChanges()
	o.Guard.Denials30d = d.guardTotal()
	o.Guard.Reasons = []JSONGuardReason{}
	for _, g := range d.Guard {
		o.Guard.Reasons = append(o.Guard.Reasons, JSONGuardReason{Reason: g.Reason, Count: g.Count, Last: g.Last.UTC().Format("2006-01-02")})
	}
	return o
}

// ---- prometheus --------------------------------------------

// promLabel escapes a label value: backslash, double quote and
// newline, as the exposition format requires.
func promLabel(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(s)
}

// Prom writes the gauges of jolitz.prom plus the hub's own.
func (d *Data) Prom(w io.Writer) {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	gauge := func(name, help string) {
		p("# HELP %s %s", name, help)
		p("# TYPE %s gauge", name)
	}
	gauge("jolitz_fleet_hosts", "Servers in memory/servers/.")
	p("jolitz_fleet_hosts %d", len(d.Hosts))
	gauge("jolitz_host_eol_days", "Days until the OS release ends support; 0 once ended, -1 when unknown.")
	for _, h := range d.Hosts {
		p(`jolitz_host_eol_days{host="%s"} %d`, promLabel(h.Name), d.EOLOf(h).Days)
	}
	gauge("jolitz_host_last_connected_days", "Days since the last connection; -1 when unknown.")
	for _, h := range d.Hosts {
		p(`jolitz_host_last_connected_days{host="%s"} %d`, promLabel(h.Name), d.ageDays(h))
	}
	gauge("jolitz_host_todos", "Open to-do items in todo.md.")
	for _, h := range d.Hosts {
		p(`jolitz_host_todos{host="%s"} %d`, promLabel(h.Name), h.Todos)
	}
	gauge("jolitz_guard_denials_30d", "Guard denials in the last 30 days.")
	p("jolitz_guard_denials_30d %d", d.guardTotal())
	gauge("jolitz_hub_sessions_open", "Open jolitz sessions known to the hub.")
	p("jolitz_hub_sessions_open %d", d.SessionsOpen)
	gauge("jolitz_hub_files", "Memory files stored in the hub.")
	p("jolitz_hub_files %d", d.Files)
	gauge("jolitz_hub_last_push_age_seconds", "Seconds since a user last pushed a file.")
	users := make([]string, 0, len(d.LastPush))
	for u := range d.LastPush {
		users = append(users, u)
	}
	sort.Strings(users)
	for _, u := range users {
		age := int64(d.now.Sub(d.LastPush[u]).Seconds())
		if age < 0 {
			age = 0
		}
		p(`jolitz_hub_last_push_age_seconds{user="%s"} %d`, promLabel(u), age)
	}
}
