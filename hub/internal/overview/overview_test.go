package overview

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// The fixture of tests/memory-tools-test.sh, as the UI tests use
// it, plus an endoflife.date answer for debian so the EOL
// section has something to say.
var fixture = map[string]string{
	"servers/web1.example.com/memory.md": `# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- SSH port: 2222
- Owner: alice
- Services: nginx 1.26, postgresql 17
- Last connected: 2026-10-28
`,
	"servers/web1.example.com/changelog.log": `[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green <script>alert(1)</script>
  Rollback: re-enable app.service.
  Flags: migrations must now stay backward-compatible.
[2026-10-27 08:00] [alice as root] nginx reloaded (auto, policy)
`,
	"servers/web1.example.com/rules.md":     "## Replace: Firewall\nnftables only.\n",
	"servers/www.example.com":               "web1.example.com\n",
	"servers/db1.example.com/memory.md":     "# db1.example.com\n- IP: 203.0.113.20\n- OS: Debian 11 (Bullseye)\n- Distro family: debian\n- Tags: prod, Database\n- Last connected: 2025-01-01\n",
	"servers/db1.example.com/todo.md":       "# To-do\n- [x] Back up pg_hba.conf\n- [ ] Upgrade to Debian 12\n",
	"servers/db1.example.com/changelog.log": "[2025-01-01 10:00] [bob as root] PostgreSQL 15 installed\n",
	"servers/mac.local/memory.md":           "# mac.local\n- OS: macOS 15.1 Sequoia\n- Mode: local\n- Last connected: yesterday\n",
	"network.md":                            "# Network\n- Link: web1.example.com -> db1.example.com (postgresql 5432)\n",
	"audits/2026-10-01-0900.json": `{"date":"2026-10-01 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"}}}
`,
	"audits/2026-10-28-0900.json": `{"date":"2026-10-28 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"yes","time.ntp":"unknown(needs-root)"}}}
`,
}

const debianEOL = `{"schema_version":"1.2.0","result":{"name":"debian","releases":[
 {"name":"12","label":"12 (Bookworm)","eolFrom":"2028-06-30"},
 {"name":"11","label":"11 (Bullseye)","eolFrom":"2026-08-31"},
 {"name":"10","label":"10 (Buster)","eolFrom":"2024-06-30"}]}}`

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Now = func() time.Time { return now }
	t.Cleanup(func() { st.Close() })
	for p, b := range fixture {
		if _, err := st.PutFile(p, []byte(b), "alice", ""); err != nil {
			t.Fatalf("fixture %s: %v", p, err)
		}
	}
	if err := st.PutEOL("debian", "2026-10-28", []byte(debianEOL)); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGuard("alice", now.Add(-time.Hour), "powering off a machine"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGuard("bob", now.Add(-40*24*time.Hour), "old denial"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSession("alice", "web1.example.com", "mbp"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestText(t *testing.T) {
	d, err := Load(newStore(t), 7)
	if err != nil {
		t.Fatal(err)
	}
	got := d.Text()
	want := `jolitz fleet summary, 2026-10-28
Hosts: 3, open to-dos: 1, changes in 7 days: 1

Changes, last 7 days
  2026-10-27 web1.example.com: nginx reloaded (auto, policy)

Open to-dos
  db1.example.com: Upgrade to Debian 12

Not connected for 30 days or more
  db1.example.com: last 2025-01-01 (665 days)

Owners
  web1.example.com: alice

End of life
  db1.example.com: Debian 11 (Bullseye), ended 2026-08-31
  some systems have no EOL data yet: the eol job fetches it daily

Fleet audit 2026-10-28 09:00: 1 settings differ, 2 changed since 2026-10-01 09:00
  db1.example.com sshd.PermitRootLogin: no -> yes
  db1.example.com time.ntp: chrony -> unknown(needs-root)

Guard denials, last 30 days: 1
  1x powering off a machine

EOL dates: endoflife.date, fetched debian 2026-10-28
`
	if got != want {
		t.Errorf("text:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "Memory lint") {
		t.Error("the hub has no lint section")
	}
}

func TestTextEmpty(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Now = func() time.Time { return now }
	d, err := Load(st, 7)
	if err != nil {
		t.Fatal(err)
	}
	got := d.Text()
	for _, w := range []string{"Hosts: 0, open to-dos: 0, changes in 7 days: 0", "Changes, last 7 days\n  none\n", "Open to-dos\n  none\n", "Not connected for 30 days or more\n  none\n", "End of life\n  none within 180 days\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("empty text lacks %q:\n%s", w, got)
		}
	}
	for _, w := range []string{"Owners", "Fleet audit", "Guard denials", "EOL dates"} {
		if strings.Contains(got, w) {
			t.Errorf("empty text has %q", w)
		}
	}
}

func TestJSONShape(t *testing.T) {
	d, err := Load(newStore(t), 30)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"generated", "today", "days", "tag", "hosts", "todos", "changes", "network", "audit", "guard", "lint"} {
		if _, ok := top[k]; !ok {
			t.Errorf("top level lacks %s", k)
		}
	}
	if len(top) != 11 {
		t.Errorf("top level has %d keys", len(top))
	}
	var hosts []map[string]json.RawMessage
	if err := json.Unmarshal(top["hosts"], &hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 || string(hosts[0]["name"]) != `"db1.example.com"` {
		t.Fatalf("hosts %s", top["hosts"])
	}
	want := []string{"name", "os", "ip", "distro_family", "last_connected", "last_connected_days", "tags", "ssh_port", "mode", "rules", "todos", "last_change", "product", "version", "owner", "aliases", "services", "eol", "audit"}
	for _, k := range want {
		if _, ok := hosts[0][k]; !ok {
			t.Errorf("host lacks %s", k)
		}
	}
	if len(hosts[0]) != len(want) {
		t.Errorf("host has %d keys, want %d", len(hosts[0]), len(want))
	}
	var web1 struct {
		Owner    string   `json:"owner"`
		Aliases  []string `json:"aliases"`
		Rules    bool     `json:"rules"`
		Services []struct {
			Name, Version string
		} `json:"services"`
		EOL struct {
			State string `json:"state"`
			Text  string `json:"text"`
			Days  int    `json:"days"`
		} `json:"eol"`
		Audit map[string]string `json:"audit"`
		Days  int               `json:"last_connected_days"`
	}
	if err := json.Unmarshal(mustHost(t, hosts, "web1.example.com"), &web1); err != nil {
		t.Fatal(err)
	}
	if web1.Owner != "alice" || len(web1.Aliases) != 1 || web1.Aliases[0] != "www.example.com" || !web1.Rules || web1.Days != 0 {
		t.Errorf("web1 %+v", web1)
	}
	if len(web1.Services) != 2 || web1.Services[1].Version != "17" {
		t.Errorf("services %+v", web1.Services)
	}
	if web1.EOL.State != "ok" || web1.EOL.Text != "until 2028-06-30" || web1.EOL.Days != 611 {
		t.Errorf("web1 eol %+v", web1.EOL)
	}
	if web1.Audit["sshd.PermitRootLogin"] != "no" {
		t.Errorf("web1 audit %v", web1.Audit)
	}
	var db1 struct {
		EOL struct {
			State string `json:"state"`
			Days  int    `json:"days"`
		} `json:"eol"`
		Todos int    `json:"todos"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(mustHost(t, hosts, "db1.example.com"), &db1); err != nil {
		t.Fatal(err)
	}
	if db1.EOL.State != "ended" || db1.EOL.Days != 0 || db1.Todos != 1 || db1.Owner != "" {
		t.Errorf("db1 %+v", db1)
	}
	var mac struct {
		EOL struct {
			State string `json:"state"`
			Days  int    `json:"days"`
		} `json:"eol"`
		Days int `json:"last_connected_days"`
	}
	if err := json.Unmarshal(mustHost(t, hosts, "mac.local"), &mac); err != nil {
		t.Fatal(err)
	}
	if mac.EOL.State != "nodata" || mac.EOL.Days != -1 || mac.Days != -1 {
		t.Errorf("mac %+v", mac)
	}
	var audit struct {
		Date     string `json:"date"`
		Previous string `json:"previous"`
		Drift    int    `json:"drift"`
		Changed  []struct {
			Host, Key, From, To string
		} `json:"changed"`
	}
	if err := json.Unmarshal(top["audit"], &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Date != "2026-10-28 09:00" || audit.Previous != "2026-10-01 09:00" || audit.Drift != 1 || len(audit.Changed) != 2 || audit.Changed[0].To != "yes" {
		t.Errorf("audit %+v", audit)
	}
	var guard struct {
		Denials int `json:"denials_30d"`
		Reasons []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
			Last   string `json:"last"`
		} `json:"reasons"`
	}
	if err := json.Unmarshal(top["guard"], &guard); err != nil {
		t.Fatal(err)
	}
	if guard.Denials != 1 || len(guard.Reasons) != 1 || guard.Reasons[0].Last != "2026-10-28" {
		t.Errorf("guard %+v", guard)
	}
	if string(top["lint"]) != "[]" || string(top["tag"]) != `""` || string(top["days"]) != "30" {
		t.Errorf("lint/tag/days: %s %s %s", top["lint"], top["tag"], top["days"])
	}
	var changes []map[string]any
	if err := json.Unmarshal(top["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 || changes[0]["date"] != "2026-10-27" {
		t.Errorf("changes %v", changes)
	}
	for _, k := range []string{"date", "time", "host", "who", "headline", "rollback", "flags", "detail"} {
		if _, ok := changes[0][k]; !ok {
			t.Errorf("change lacks %s", k)
		}
	}
	var network []map[string]string
	if err := json.Unmarshal(top["network"], &network); err != nil {
		t.Fatal(err)
	}
	if len(network) != 1 || network[0]["from"] != "web1.example.com" || network[0]["to"] != "db1.example.com" || network[0]["label"] != "postgresql 5432" {
		t.Errorf("network %v", network)
	}
	var todos []map[string]string
	if err := json.Unmarshal(top["todos"], &todos); err != nil {
		t.Fatal(err)
	}
	if len(todos) != 1 || todos[0]["host"] != "db1.example.com" || todos[0]["text"] != "Upgrade to Debian 12" {
		t.Errorf("todos %v", todos)
	}
}

func mustHost(t *testing.T, hosts []map[string]json.RawMessage, name string) []byte {
	t.Helper()
	for _, h := range hosts {
		if string(h["name"]) == `"`+name+`"` {
			b, _ := json.Marshal(h)
			return b
		}
	}
	t.Fatalf("no host %s", name)
	return nil
}

var (
	sampleRE = regexp.MustCompile(`^([a-z_][a-z0-9_]*)(\{[a-z_]+="(?:[^"\\]|\\.)*"\})? (-?[0-9]+)$`)
	helpRE   = regexp.MustCompile(`^# HELP ([a-z_][a-z0-9_]*) .+$`)
	typeRE   = regexp.MustCompile(`^# TYPE ([a-z_][a-z0-9_]*) gauge$`)
)

func TestProm(t *testing.T) {
	d, err := Load(newStore(t), 30)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	d.Prom(&buf)
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Error("output does not end in a newline")
	}
	declared := map[string]int{} // name -> 0 none, 1 help, 2 help+type
	seen := map[string]bool{}
	samples := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		switch {
		case helpRE.MatchString(line):
			name := helpRE.FindStringSubmatch(line)[1]
			if declared[name] != 0 || seen[name] {
				t.Errorf("HELP for %s repeated or after a sample", name)
			}
			declared[name] = 1
		case typeRE.MatchString(line):
			name := typeRE.FindStringSubmatch(line)[1]
			if declared[name] != 1 {
				t.Errorf("TYPE for %s without HELP first", name)
			}
			declared[name] = 2
		case sampleRE.MatchString(line):
			m := sampleRE.FindStringSubmatch(line)
			if declared[m[1]] != 2 {
				t.Errorf("sample %s before HELP/TYPE", line)
			}
			seen[m[1]] = true
			samples[m[1]+m[2]] = m[3]
		default:
			t.Errorf("line %q is neither HELP, TYPE nor a sample", line)
		}
	}
	for _, name := range []string{"jolitz_fleet_hosts", "jolitz_host_eol_days", "jolitz_host_last_connected_days", "jolitz_host_todos", "jolitz_guard_denials_30d", "jolitz_hub_sessions_open", "jolitz_hub_files", "jolitz_hub_last_push_age_seconds"} {
		if declared[name] != 2 {
			t.Errorf("%s not declared", name)
		}
	}
	if len(declared) != 8 {
		t.Errorf("%d metrics declared", len(declared))
	}
	for k, v := range map[string]string{
		"jolitz_fleet_hosts":                                       "3",
		`jolitz_host_todos{host="db1.example.com"}`:                "1",
		`jolitz_host_eol_days{host="db1.example.com"}`:             "0",
		`jolitz_host_eol_days{host="web1.example.com"}`:            "611",
		`jolitz_host_eol_days{host="mac.local"}`:                   "-1",
		`jolitz_host_last_connected_days{host="mac.local"}`:        "-1",
		`jolitz_host_last_connected_days{host="web1.example.com"}`: "0",
		`jolitz_host_last_connected_days{host="db1.example.com"}`:  "665",
		"jolitz_guard_denials_30d":                                 "1",
		"jolitz_hub_sessions_open":                                 "1",
		"jolitz_hub_files":                                         "11",
		`jolitz_hub_last_push_age_seconds{user="alice"}`:           "0",
	} {
		if samples[k] != v {
			t.Errorf("%s = %q, want %s", k, samples[k], v)
		}
	}
}

func TestEOLCycleMatching(t *testing.T) {
	prod := parseEOL([]byte(`{"result":{"releases":[{"name":"24.04","eolFrom":"2029-04-25"},{"name":"9","eolFrom":""},{"name":"15","eolFrom":"2027-01-31"}]}}`))
	today := dnum("2026-10-28")
	for _, c := range []struct {
		version, state, text string
		days                 int
	}{
		{"24.04.2", "ok", "until 2029-04-25", dnum("2029-04-25") - today},
		{"24.04", "ok", "until 2029-04-25", dnum("2029-04-25") - today},
		{"9.5", "open", "no end date announced", -1},
		{"15.1", "soon", "ends 2027-01-31 (95 days)", 95},
		{"22.04", "unlisted", "release 22.04 not listed for x", -1},
	} {
		e := eolOf(map[string]map[string]string{"x": prod}, "x", c.version, today)
		if e.State != c.state || e.Text != c.text || e.Days != c.days {
			t.Errorf("%s: %+v, want %s %q %d", c.version, e, c.state, c.text, c.days)
		}
	}
	e := eolOf(map[string]map[string]string{}, "", "1", today)
	if e.State != "" || e.Days != -1 {
		t.Errorf("no product: %+v", e)
	}
	e = eolOf(map[string]map[string]string{}, "debian", "12", today)
	if e.State != "nodata" {
		t.Errorf("uncached product: %+v", e)
	}
	if parseEOL([]byte("not json")) != nil {
		t.Error("garbage parsed")
	}
}
