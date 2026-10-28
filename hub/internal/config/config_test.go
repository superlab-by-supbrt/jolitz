package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.Listen != "127.0.0.1:7331" || c.DataDir != "/var/lib/jolitz-hub" || c.BaseURL != "" || c.MetricsPublic {
		t.Errorf("defaults %+v", c)
	}
	if c.Retention.SessionsDays != 90 || c.Retention.GuardDays != 30 || c.Retention.VersionsYears != 2 {
		t.Errorf("retention defaults %+v", c.Retention)
	}
	if !c.EOL.Enabled || c.Summary.Weekday != "mon" || c.Summary.Hour != 7 {
		t.Errorf("eol/summary defaults %+v %+v", c.EOL, c.Summary)
	}
	if c.Notify.Ntfy.Server != "https://ntfy.sh" || c.Notify.Ntfy.Topic != "" {
		t.Errorf("ntfy defaults %+v", c.Notify.Ntfy)
	}
	if c.Notify.Ntfy.Enabled() || c.Notify.Matrix.Enabled() || c.Notify.Webhook.Enabled() {
		t.Error("a channel is enabled without configuration")
	}
}

const full = `# jolitz-hub configuration
listen = "0.0.0.0:8080"   # trailing comment
base_url = "https://hub.example.com"
data_dir = "/srv/hub"
metrics_public = true

[retention]
sessions_days = 10
guard_days = 5
versions_years = 1

[eol]
enabled = false

[summary]
weekday = "Fri"
hour = 18

[notify.ntfy]
server = "https://ntfy.example.com/"
topic = "jolitz-fleet-a8f3"
header_file = "ntfy-auth"

[notify.matrix]
server = "https://matrix.example.org"
room = "!abc123:example.org"
header_file = "/etc/jolitz-hub/matrix-auth"

[notify.webhook]
curlrc_file = "webhook.curlrc"
`

func TestParseFull(t *testing.T) {
	c, err := Parse(strings.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "0.0.0.0:8080" || c.BaseURL != "https://hub.example.com" || c.DataDir != "/srv/hub" || !c.MetricsPublic {
		t.Errorf("top level %+v", c)
	}
	if c.Retention.SessionsDays != 10 || c.Retention.GuardDays != 5 || c.Retention.VersionsYears != 1 {
		t.Errorf("retention %+v", c.Retention)
	}
	if c.EOL.Enabled || c.Summary.Weekday != "fri" || c.Summary.Hour != 18 {
		t.Errorf("eol/summary %+v %+v", c.EOL, c.Summary)
	}
	if c.Notify.Ntfy.Server != "https://ntfy.example.com" || c.Notify.Ntfy.Topic != "jolitz-fleet-a8f3" {
		t.Errorf("ntfy %+v", c.Notify.Ntfy)
	}
	// Relative secret files resolve against the data dir.
	if c.Notify.Ntfy.HeaderFile != "/srv/hub/ntfy-auth" || c.Notify.Matrix.HeaderFile != "/etc/jolitz-hub/matrix-auth" || c.Notify.Webhook.CurlrcFile != "/srv/hub/webhook.curlrc" {
		t.Errorf("secret files %+v", c.Notify)
	}
	if c.Notify.Matrix.Room != "!abc123:example.org" {
		t.Errorf("matrix %+v", c.Notify.Matrix)
	}
	if !c.Notify.Ntfy.Enabled() || !c.Notify.Matrix.Enabled() || !c.Notify.Webhook.Enabled() {
		t.Error("configured channel not enabled")
	}
	if len(c.Channels()) != 3 {
		t.Errorf("channels %v", c.Channels())
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"listen = 7331\n", "line 1: listen takes a string"},
		{"[retention]\nsessions_days = \"90\"\n", "line 2: sessions_days takes a number"},
		{"metrics_public = yes\n", "line 1"},
		{"[nowhere]\nx = 1\n", "line 1: unknown section"},
		{"colour = \"red\"\n", "line 1: unknown key"},
		{"listen = \"a\" \"b\"\n", "line 1"},
		{"[summary]\nweekday = \"someday\"\n", "weekday"},
		{"[summary]\nhour = 24\n", "hour"},
		{"[retention]\nguard_days = 0\n", "guard_days"},
		{"[notify.ntfy]\nserver = \"\"\n", "server"},
		{"[notify.matrix]\nroom = \"!r:x\"\n", "matrix needs server, room and header_file"},
		{"listen = \"x\" = \"y\"\n", "line 1"},
		{"= \"x\"\n", "line 1"},
		{"[retention\nx = 1\n", "line 1"},
		{"listen = \"unterminated\n", "line 1"},
	} {
		_, err := Parse(strings.NewReader(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.in, err, c.want)
		}
	}
}

func TestStringEscapesAndTypes(t *testing.T) {
	c, err := Parse(strings.NewReader("base_url = \"https://h/\\\"q\\\" \\\\ \\t\"\n[summary]\nhour = 0\nweekday = \"SUN\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://h/\"q\" \\ \t" {
		t.Errorf("escapes: %q", c.BaseURL)
	}
	if c.Summary.Hour != 0 || c.Summary.Weekday != "sun" {
		t.Errorf("summary %+v", c.Summary)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hub.toml")
	if err := os.WriteFile(p, []byte("listen = \"127.0.0.1:1\"\n[notify.ntfy]\ntopic = \"t\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:1" || c.Notify.Ntfy.Topic != "t" || c.Path != p {
		t.Errorf("loaded %+v", c)
	}
	if _, err := Load(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("missing file loads")
	}
	// A CRLF file parses too.
	if err := os.WriteFile(p, []byte("listen = \"127.0.0.1:2\"\r\n[eol]\r\nenabled = false\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil || c.Listen != "127.0.0.1:2" || c.EOL.Enabled {
		t.Errorf("crlf: %+v %v", c, err)
	}
}

func TestPublic(t *testing.T) {
	c, err := Parse(strings.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	pub := c.Public()
	var keys []string
	for _, kv := range pub {
		keys = append(keys, kv.Key)
		if strings.Contains(kv.Value, "abc123") && kv.Key != "notify.matrix.room" {
			t.Errorf("%s leaks the room id", kv.Key)
		}
	}
	joined := strings.Join(keys, " ")
	for _, want := range []string{"listen", "data_dir", "retention.sessions_days", "eol.enabled", "summary.weekday", "notify.ntfy.topic", "notify.ntfy.header_file", "notify.matrix.room", "notify.webhook.curlrc_file", "metrics_public"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Public lacks %s: %s", want, joined)
		}
	}
}

// The annotated example shipped as hub/hub.toml loads and says
// exactly what the defaults say.
func TestExampleFileIsTheDefault(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "hub.toml"))
	if err != nil {
		t.Fatal(err)
	}
	c.Path = ""
	d := Default()
	if *c != *d {
		t.Errorf("hub.toml differs from the defaults:\n%+v\n%+v", c, d)
	}
}
