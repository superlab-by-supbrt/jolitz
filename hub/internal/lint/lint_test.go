package lint

import (
	"strings"
	"testing"
)

// The fixtures are assembled at runtime, as tests/memory-tools-
// test.sh does, so no token-shaped string sits in the repository.
func TestHits(t *testing.T) {
	ghp := "gh" + "p_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	key := "-----BEGIN OPENSSH " + "PRIVATE KEY-----"
	cases := map[string]string{
		"github token":     "- Deploy token: " + ghp + "\n",
		"private key":      "[2026-01-01 10:00] x\n  Detail: " + key + "\n",
		"assignment":       "# notes\nDB_" + "PASSWORD=hunter2\n",
		"url password":     "mysql://app:s3c" + "ret@db1/app\n",
		"assignment later": "2026-10-28 alice: asked for the root " + "password=abc123\n",
		"aws key":          "- Key: AKIA" + strings.Repeat("A", 16) + "\n",
		"slack":            "xoxb-" + strings.Repeat("1", 12) + "\n",
		"sha512 hash":      "root:$" + "6$" + "saltsalt$" + strings.Repeat("a", 24) + "\n",
		"anthropic":        "sk-" + "ant-" + strings.Repeat("x", 40) + "\n",
		"api key assign":   "API_KEY = x" + strings.Repeat("y", 10) + "\n",
	}
	for name, body := range cases {
		h := Check([]byte(body))
		if h == nil {
			t.Errorf("%s: no hit", name)
			continue
		}
		if h.Reason != Reason {
			t.Errorf("%s: reason %q", name, h.Reason)
		}
	}
}

func TestLineNumber(t *testing.T) {
	ghp := "gh" + "p_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	body := "# web1\n- IP: 1.2.3.4\n- Token: " + ghp + "\n"
	h := Check([]byte(body))
	if h == nil || h.Line != 3 {
		t.Fatalf("hit = %+v, want line 3", h)
	}
	// Only the first hit is reported.
	body += "- Second: " + ghp + "\n"
	if h := Check([]byte(body)); h.Line != 3 {
		t.Errorf("first hit line = %d", h.Line)
	}
}

// A line longer than any scanner buffer is still checked to its
// end: the check must not fail open on size.
func TestLongLine(t *testing.T) {
	ghp := "gh" + "p_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	body := "# web1\n" + strings.Repeat("a", 17<<20) + " " + ghp + "\n"
	h := Check([]byte(body))
	if h == nil || h.Line != 2 {
		t.Fatalf("hit = %+v, want line 2", h)
	}
}

func TestPasses(t *testing.T) {
	cases := map[string]string{
		"location":        "- Database password: in /etc/app/db.env (0600 root)\n",
		"word after":      "- Password: hunter2x\n- Token: expires 2027-01-01\n",
		"url no pass":     "https://hub.example.com/api/v1/health\n",
		"url with port":   "postgresql://db1:5432/app\n",
		"token in word":   "xghp_" + strings.Repeat("a", 36) + "\n",
		"variable":        "PASSWORD=$DB_PASSWORD\n",
		"placeholder":     "token=<paste here>\n",
		"empty":           "",
		"headline":        "app.example.com now deploys blue/green <script>alert(1)</script>\n",
		"quote backslash": `quoted "value" and back\slash` + "\n",
		"short key":       "sk-short\n",
	}
	for name, body := range cases {
		if h := Check([]byte(body)); h != nil {
			t.Errorf("%s: hit on line %d", name, h.Line)
		}
	}
}
