package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBAndOIDCDefaults(t *testing.T) {
	c := Default()
	if c.DB != "sqlite" || c.DBDSNFile != "" {
		t.Errorf("db defaults %q %q", c.DB, c.DBDSNFile)
	}
	if c.OIDC.Enabled() || c.OIDC.UsernameClaim != "preferred_username" {
		t.Errorf("oidc defaults %+v", c.OIDC)
	}
}

func TestParseDBAndOIDC(t *testing.T) {
	c, err := Parse(strings.NewReader(`data_dir = "/srv/hub"
base_url = "https://hub.example.com/"
db = "postgres"
db_dsn_file = "db.dsn"

[oidc]
issuer = "https://id.example.com/realms/ops"
client_id = "jolitz-hub"
client_secret_file = "/etc/jolitz-hub/oidc-secret"
username_claim = "email"
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.DB != "postgres" || c.DBDSNFile != "/srv/hub/db.dsn" {
		t.Errorf("db %q %q", c.DB, c.DBDSNFile)
	}
	o := c.OIDC
	if !o.Enabled() || o.Issuer != "https://id.example.com/realms/ops" || o.ClientID != "jolitz-hub" ||
		o.ClientSecretFile != "/etc/jolitz-hub/oidc-secret" || o.UsernameClaim != "email" {
		t.Errorf("oidc %+v", o)
	}
	if got := c.OIDCRedirectURL(); got != "https://hub.example.com/oidc/callback" {
		t.Errorf("redirect %q", got)
	}
	pub := fmt.Sprint(c.Public())
	for _, want := range []string{"db postgres", "db_dsn_file /srv/hub/db.dsn", "oidc.issuer", "oidc.client_secret_file /etc/jolitz-hub/oidc-secret", "oidc.redirect_url https://hub.example.com/oidc/callback"} {
		if !strings.Contains(pub, want) {
			t.Errorf("Public lacks %q: %s", want, pub)
		}
	}
}

func TestDBAndOIDCErrors(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"db = \"mysql\"\n", "db: sqlite or postgres"},
		{"db = \"postgres\"\n", "db = \"postgres\" needs db_dsn_file"},
		{"db_dsn_file = \"x\"\n", "db_dsn_file needs db = \"postgres\""},
		{"db = \"postgres://u:pw@h/db\"\n", "the DSN belongs in the file db_dsn_file names"},
		{"[oidc]\nclient_id = \"x\"\n", "oidc needs issuer and client_id"},
		{"base_url = \"https://h\"\n[oidc]\nissuer = \"https://id\"\n", "oidc needs issuer and client_id"},
		{"base_url = \"https://h\"\n[oidc]\nissuer = \"http://id.example.com\"\nclient_id = \"x\"\n", "oidc.issuer must be https"},
		{"[oidc]\nissuer = \"https://id\"\nclient_id = \"x\"\n", "oidc needs base_url"},
		{"base_url = \"https://h\"\n[oidc]\nissuer = \"https://id\"\nclient_id = \"x\"\nusername_claim = \"\"\n", "username_claim"},
		{"[oidc]\ncolour = \"x\"\n", "unknown key oidc.colour"},
	} {
		_, err := Parse(strings.NewReader(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.in, err, c.want)
		}
	}
	// A loopback issuer may speak plain HTTP (tests, an IdP on
	// the same host).
	if _, err := Parse(strings.NewReader("base_url = \"http://127.0.0.1:7331\"\n[oidc]\nissuer = \"http://127.0.0.1:8080\"\nclient_id = \"x\"\n")); err != nil {
		t.Errorf("loopback issuer: %v", err)
	}
}

func TestReadSecretFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "db.dsn")
	if err := os.WriteFile(p, []byte("postgres://hub:s3cret@db1/hub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warned []string
	logf := func(f string, a ...any) { warned = append(warned, fmt.Sprintf(f, a...)) }
	v, err := ReadSecretFile(p, logf)
	if err != nil || v != "postgres://hub:s3cret@db1/hub" || len(warned) != 0 {
		t.Errorf("read %q %v %v", v, err, warned)
	}
	os.Chmod(p, 0o640)
	if _, err := ReadSecretFile(p, logf); err != nil || len(warned) != 1 || !strings.Contains(warned[0], "0640") {
		t.Errorf("group-readable: %v %v", err, warned)
	}
	for _, w := range warned {
		if strings.Contains(w, "s3cret") {
			t.Errorf("warning shows the secret: %s", w)
		}
	}
	os.WriteFile(p, []byte("\n  \n"), 0o600)
	if _, err := ReadSecretFile(p, logf); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty file: %v", err)
	}
	_, err = ReadSecretFile(filepath.Join(dir, "missing"), logf)
	if err == nil {
		t.Error("missing file reads")
	}
}

// --data on the command line moves the relative secret files
// with it.
func TestSetDataDirResolvesAgain(t *testing.T) {
	c, err := Parse(strings.NewReader("db = \"postgres\"\ndb_dsn_file = \"db.dsn\"\n[notify.ntfy]\ntopic = \"t\"\nheader_file = \"/abs/ntfy\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DBDSNFile != "/var/lib/jolitz-hub/db.dsn" {
		t.Errorf("before: %s", c.DBDSNFile)
	}
	c.SetDataDir("/srv/hub")
	if c.DBDSNFile != "/srv/hub/db.dsn" || c.Notify.Ntfy.HeaderFile != "/abs/ntfy" {
		t.Errorf("after: %s %s", c.DBDSNFile, c.Notify.Ntfy.HeaderFile)
	}
}
