package parse

import (
	"reflect"
	"testing"
)

func TestMemoryWeb1(t *testing.T) {
	m := Memory([]byte(web1Memory))
	want := Host{
		Heading:       "web1.example.com",
		OS:            "Debian 12 (Bookworm)",
		IP:            "203.0.113.10",
		Distro:        "debian",
		Tags:          []string{"prod", "web"},
		SSHPort:       "2222",
		Owner:         "alice",
		Services:      []Service{{"nginx", "1.26"}, {"postgresql", "17"}},
		LastConnected: "2026-10-28",
		Product:       "debian",
		Version:       "12",
	}
	got := m
	got.Fields = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Memory(web1) = %+v\nwant %+v", got, want)
	}
	// The password line is a fact about a location, kept as a
	// field like every other "- Key: value" line.
	if m.Fields["Database password"] != "in /etc/app/db.env (0600 root)" {
		t.Errorf("Fields[Database password] = %q", m.Fields["Database password"])
	}
}

func TestMemoryDb1AndMac(t *testing.T) {
	d := Memory([]byte(db1Memory))
	if !reflect.DeepEqual(d.Tags, []string{"prod", "Database"}) {
		t.Errorf("db1 tags = %v", d.Tags)
	}
	if d.Owner != "" || len(d.Services) != 0 || d.Product != "debian" || d.Version != "11" {
		t.Errorf("db1 = %+v", d)
	}
	m := Memory([]byte(macMemory))
	if m.Mode != "local" || m.LastConnected != "yesterday" || m.Product != "macos" || m.Version != "15.1" {
		t.Errorf("mac = %+v", m)
	}
	if m.IP != "" || m.Heading != "mac.local" {
		t.Errorf("mac = %+v", m)
	}
}

func TestMemoryFirstValueWinsAndGuest(t *testing.T) {
	m := Memory([]byte("# h\n- OS: SLES 15 SP5\n- OS: other\n- Guest: sap, oracle\n- Last connected: 2026-01-01 10:00 by alice\n\tnot a fact\n- Services: docker\n"))
	if m.OS != "SLES 15 SP5" {
		t.Errorf("OS = %q", m.OS)
	}
	if !reflect.DeepEqual(m.Guest, []string{"sap", "oracle"}) {
		t.Errorf("Guest = %v", m.Guest)
	}
	if m.LastConnected != "2026-01-01" {
		t.Errorf("LastConnected = %q", m.LastConnected)
	}
	if m.Product != "sles" || m.Version != "15.5" {
		t.Errorf("product/version = %q/%q", m.Product, m.Version)
	}
	if !reflect.DeepEqual(m.Services, []Service{{"docker", ""}}) {
		t.Errorf("Services = %v", m.Services)
	}
}

func TestChangelogWeb1(t *testing.T) {
	cs := Changelog("web1.example.com", []byte(web1Changelog))
	if len(cs) != 3 {
		t.Fatalf("got %d entries, want 3", len(cs))
	}
	c := cs[0]
	if c.Date != "2026-06-11" || c.Time != "09:50" || c.Host != "web1.example.com" || c.Who != "alice as root" {
		t.Errorf("header = %+v", c)
	}
	wantHead := "app.example.com now deploys blue/green <script>alert(1)</script> — because deploys interrupted visitors"
	if c.Headline != wantHead {
		t.Errorf("headline = %q", c.Headline)
	}
	if c.Rollback != "re-enable app.service, restore vhost backup from /var/backups/jolitz/." {
		t.Errorf("rollback = %q", c.Rollback)
	}
	if c.Flags != "migrations must now stay backward-compatible." {
		t.Errorf("flags = %q", c.Flags)
	}
	wantDetail := []string{
		"Detail: templated app@.service (blue :4003,",
		"green :4005).",
		"Rollback: re-enable app.service, restore vhost",
		"backup from /var/backups/jolitz/.",
		"Verify: 135 probes, all 200.",
		"Flags: migrations must now stay",
		"backward-compatible.",
	}
	if !reflect.DeepEqual(c.Detail, wantDetail) {
		t.Errorf("detail = %q", c.Detail)
	}
	if cs[1].Headline != "nginx reloaded (auto, policy)" || cs[1].Detail != nil {
		t.Errorf("second = %+v", cs[1])
	}
	if cs[2].Headline != `quoted "value" and back\slash` || cs[2].Time != "08:00" {
		t.Errorf("third = %+v", cs[2])
	}
}

func TestChangelogDb1(t *testing.T) {
	cs := Changelog("db1.example.com", []byte(db1Changelog))
	if len(cs) != 1 {
		t.Fatalf("got %d entries, want 1", len(cs))
	}
	// A line without the full timestamp is no new entry; like the
	// awk it continues the headline before it.
	if cs[0].Headline != "PostgreSQL 15 installed [2025-01-02] forgot the timestamp format" {
		t.Errorf("headline = %q", cs[0].Headline)
	}
	if cs[0].Who != "bob as root" {
		t.Errorf("who = %q", cs[0].Who)
	}
	if got := Changelog("x", []byte("prose before the first entry\n")); len(got) != 0 {
		t.Errorf("prose only = %+v", got)
	}
}

func TestTodos(t *testing.T) {
	got := Todos([]byte(db1Todo))
	want := []Todo{{"Back up pg_hba.conf", false}, {"Upgrade to Debian 12", true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Todos = %+v", got)
	}
	if n := Todos([]byte("  - [ ] indented\n- [X] upper\n- plain\n")); len(n) != 2 || !n[0].Open || n[1].Open {
		t.Errorf("Todos variants = %+v", n)
	}
}

func TestNetwork(t *testing.T) {
	got := Network([]byte(network))
	want := []Link{{"web1.example.com", "db1.example.com", "postgresql 5432"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Network = %+v", got)
	}
	if got := Network([]byte("# Network\nOnly prose here, no link lines.\n")); len(got) != 0 {
		t.Errorf("prose = %+v", got)
	}
	got = Network([]byte("- Link: a -> b\n- Link: c->d (x (y))\n- Link: e ->\n"))
	want = []Link{{"a", "b", ""}, {"c", "d", "x (y)"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Network variants = %+v", got)
	}
}

func TestAudit(t *testing.T) {
	date, vals, err := Audit([]byte(auditNew))
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-10-28 09:00" {
		t.Errorf("date = %q", date)
	}
	want := []AuditValue{
		{"db1.example.com", "sshd.PermitRootLogin", "yes"},
		{"db1.example.com", "time.ntp", "unknown(needs-root)"},
		{"web1.example.com", "sshd.PermitRootLogin", "no"},
		{"web1.example.com", "time.ntp", "chrony"},
	}
	if !reflect.DeepEqual(vals, want) {
		t.Errorf("Audit = %+v", vals)
	}
	if _, _, err := Audit([]byte("{")); err == nil {
		t.Error("broken JSON parsed")
	}
	if _, vals, err := Audit([]byte(`{"date":"d","hosts":{"h":{"n":1,"b":true}}}`)); err != nil || vals[1].Value != "1" || vals[0].Value != "true" {
		t.Errorf("non-string values = %+v %v", vals, err)
	}
}

func TestAlias(t *testing.T) {
	if got := Alias([]byte("web1.example.com\r\n")); got != "web1.example.com" {
		t.Errorf("Alias = %q", got)
	}
	for _, bad := range []string{"", "a/b", ".hidden", "two lines\nmore", "<x>"} {
		if got := Alias([]byte(bad)); got != "" {
			t.Errorf("Alias(%q) = %q, want empty", bad, got)
		}
	}
}

func TestHostName(t *testing.T) {
	for _, ok := range []string{"web1.example.com", "a", "1.2.3.4", "x-y.z"} {
		if !ValidHost(ok) {
			t.Errorf("ValidHost(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "-a", ".a", "a/b", "a<b", `a"b`, "a b", "a_b", "..", "a\n"} {
		if ValidHost(bad) {
			t.Errorf("ValidHost(%q) = true", bad)
		}
	}
}
