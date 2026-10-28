// Package config reads hub.toml. The file is optional; without
// it the hub runs with the defaults below. Only a subset of TOML
// is understood, enough for a flat config with sections:
//
//	key = "string"      with the escapes \" \\ \t \n
//	key = 123           an integer, optionally negative
//	key = true          or false
//	[section]           and dotted names such as [notify.ntfy]
//	# comment           on its own line or after a value
//
// No arrays, no inline tables, no multi-line strings, no dates.
// Unknown keys and sections are errors, so a typo cannot silently
// disable a job. Secrets are never in this file: it names the
// files that hold them, which the hub reads at send time.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the effective configuration of one hub.
type Config struct {
	// Path is the file this was read from; "" for the defaults.
	Path string

	Listen        string
	BaseURL       string
	DataDir       string
	MetricsPublic bool

	Retention Retention
	EOL       EOL
	Summary   Summary
	Notify    Notify
}

// Retention: how long the hub keeps what only it knows. File
// versions are not implemented in phase 1; the key is read and
// shown so a config written for a later release loads today.
type Retention struct {
	SessionsDays  int
	GuardDays     int
	VersionsYears int
}

// EOL switches the daily fetch from endoflife.date.
type EOL struct {
	Enabled bool
}

// Summary says when the weekly fleet summary goes out: a weekday
// (mon..sun) and an hour (0..23) in the hub host's local time.
type Summary struct {
	Weekday string
	Hour    int
}

// Notify holds the three channels of rules/notify.md. A channel
// is enabled when its required keys are set.
type Notify struct {
	Ntfy    Ntfy
	Matrix  Matrix
	Webhook Webhook
}

// Ntfy publishes to one topic. HeaderFile is optional and holds
// one HTTP header per line, usually Authorization.
type Ntfy struct {
	Server     string
	Topic      string
	HeaderFile string
}

// Enabled reports whether a topic is configured.
func (n Ntfy) Enabled() bool { return n.Topic != "" }

// Matrix posts m.text events into one room with the access token
// from HeaderFile.
type Matrix struct {
	Server     string
	Room       string
	HeaderFile string
}

// Enabled reports whether the room is configured.
func (m Matrix) Enabled() bool { return m.Room != "" }

// Webhook posts Slack-shaped JSON to the url = "…" line of a
// curlrc file; the URL is the secret.
type Webhook struct {
	CurlrcFile string
}

// Enabled reports whether a curlrc is configured.
func (w Webhook) Enabled() bool { return w.CurlrcFile != "" }

// Default returns the configuration without a file.
func Default() *Config {
	return &Config{
		Listen:    "127.0.0.1:7331",
		DataDir:   "/var/lib/jolitz-hub",
		Retention: Retention{SessionsDays: 90, GuardDays: 30, VersionsYears: 2},
		EOL:       EOL{Enabled: true},
		Summary:   Summary{Weekday: "mon", Hour: 7},
		Notify:    Notify{Ntfy: Ntfy{Server: "https://ntfy.sh"}},
	}
}

// Load reads and validates a file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

// Parse reads the TOML subset from r on top of the defaults.
func Parse(r io.Reader) (*Config, error) {
	c := Default()
	sc := bufio.NewScanner(r)
	section := ""
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(strings.TrimSuffix(sc.Text(), "\r"))
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 || strings.TrimSpace(stripComment(line[end+1:])) != "" {
				return nil, fmt.Errorf("line %d: malformed section header", n)
			}
			section = strings.TrimSpace(line[1:end])
			if !knownSection(section) {
				return nil, fmt.Errorf("line %d: unknown section [%s]", n, section)
			}
			continue
		}
		key, rest, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t\"'") {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		v, err := parseValue(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		if err := c.set(section, key, v); err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	c.resolve()
	return c, nil
}

func knownSection(s string) bool {
	switch s {
	case "retention", "eol", "summary", "notify.ntfy", "notify.matrix", "notify.webhook":
		return true
	}
	return false
}

// value is one parsed right-hand side.
type value struct {
	kind string // "string", "int", "bool"
	s    string
	i    int
	b    bool
}

// stripComment removes a # comment from text that holds no
// string literal.
func stripComment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}

func parseValue(s string) (value, error) {
	if s == "" {
		return value{}, errors.New("missing value")
	}
	if s[0] == '"' {
		var b strings.Builder
		i := 1
		for ; i < len(s); i++ {
			ch := s[i]
			if ch == '"' {
				break
			}
			if ch == '\\' {
				i++
				if i >= len(s) {
					return value{}, errors.New("unterminated string")
				}
				switch s[i] {
				case '"', '\\':
					b.WriteByte(s[i])
				case 't':
					b.WriteByte('\t')
				case 'n':
					b.WriteByte('\n')
				default:
					return value{}, fmt.Errorf("unknown escape \\%c", s[i])
				}
				continue
			}
			b.WriteByte(ch)
		}
		if i >= len(s) {
			return value{}, errors.New("unterminated string")
		}
		if strings.TrimSpace(stripComment(s[i+1:])) != "" {
			return value{}, errors.New("text after the closing quote")
		}
		return value{kind: "string", s: b.String()}, nil
	}
	word := strings.TrimSpace(stripComment(s))
	if strings.ContainsAny(word, " \t") {
		return value{}, errors.New("one value per line")
	}
	switch word {
	case "true":
		return value{kind: "bool", b: true}, nil
	case "false":
		return value{kind: "bool", b: false}, nil
	}
	i, err := strconv.Atoi(word)
	if err != nil {
		return value{}, fmt.Errorf("value %q is neither a quoted string, an integer nor true/false", word)
	}
	return value{kind: "int", i: i}, nil
}

func (c *Config) set(section, key string, v value) error {
	str := func(p *string) error {
		if v.kind != "string" {
			return fmt.Errorf("%s takes a string", key)
		}
		*p = v.s
		return nil
	}
	num := func(p *int) error {
		if v.kind != "int" {
			return fmt.Errorf("%s takes a number", key)
		}
		*p = v.i
		return nil
	}
	boolean := func(p *bool) error {
		if v.kind != "bool" {
			return fmt.Errorf("%s takes true or false", key)
		}
		*p = v.b
		return nil
	}
	full := key
	if section != "" {
		full = section + "." + key
	}
	switch full {
	case "listen":
		return str(&c.Listen)
	case "base_url":
		return str(&c.BaseURL)
	case "data_dir":
		return str(&c.DataDir)
	case "metrics_public":
		return boolean(&c.MetricsPublic)
	case "retention.sessions_days":
		return num(&c.Retention.SessionsDays)
	case "retention.guard_days":
		return num(&c.Retention.GuardDays)
	case "retention.versions_years":
		return num(&c.Retention.VersionsYears)
	case "eol.enabled":
		return boolean(&c.EOL.Enabled)
	case "summary.weekday":
		return str(&c.Summary.Weekday)
	case "summary.hour":
		return num(&c.Summary.Hour)
	case "notify.ntfy.server":
		return str(&c.Notify.Ntfy.Server)
	case "notify.ntfy.topic":
		return str(&c.Notify.Ntfy.Topic)
	case "notify.ntfy.header_file":
		return str(&c.Notify.Ntfy.HeaderFile)
	case "notify.matrix.server":
		return str(&c.Notify.Matrix.Server)
	case "notify.matrix.room":
		return str(&c.Notify.Matrix.Room)
	case "notify.matrix.header_file":
		return str(&c.Notify.Matrix.HeaderFile)
	case "notify.webhook.curlrc_file":
		return str(&c.Notify.Webhook.CurlrcFile)
	}
	return fmt.Errorf("unknown key %s", full)
}

// Weekdays as the config spells them, Sunday first as in
// time.Weekday.
var Weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// WeekdayIndex maps "mon", "Monday" or "MON" to time.Weekday's
// number; -1 when it is no weekday.
func WeekdayIndex(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return -1
	}
	for i, d := range Weekdays {
		if s == d || (len(s) > 3 && strings.HasPrefix(s, d)) {
			return i
		}
	}
	return -1
}

func (c *Config) validate() error {
	if c.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data_dir must not be empty")
	}
	if c.Retention.SessionsDays < 1 {
		return errors.New("retention.sessions_days must be at least 1")
	}
	if c.Retention.GuardDays < 1 {
		return errors.New("retention.guard_days must be at least 1")
	}
	if c.Retention.VersionsYears < 1 {
		return errors.New("retention.versions_years must be at least 1")
	}
	wd := WeekdayIndex(c.Summary.Weekday)
	if wd < 0 {
		return fmt.Errorf("summary.weekday %q: use mon, tue, wed, thu, fri, sat or sun", c.Summary.Weekday)
	}
	c.Summary.Weekday = Weekdays[wd]
	if c.Summary.Hour < 0 || c.Summary.Hour > 23 {
		return errors.New("summary.hour must be 0..23")
	}
	c.Notify.Ntfy.Server = strings.TrimRight(c.Notify.Ntfy.Server, "/")
	if c.Notify.Ntfy.Server == "" {
		return errors.New("notify.ntfy server must not be empty (default https://ntfy.sh)")
	}
	if c.Notify.Ntfy.Topic != "" && strings.ContainsAny(c.Notify.Ntfy.Topic, "/ \t") {
		return errors.New("notify.ntfy topic is one path segment")
	}
	m := &c.Notify.Matrix
	m.Server = strings.TrimRight(m.Server, "/")
	if (m.Server != "" || m.Room != "" || m.HeaderFile != "") && (m.Server == "" || m.Room == "" || m.HeaderFile == "") {
		return errors.New("notify.matrix needs server, room and header_file")
	}
	return nil
}

// resolve makes the secret file paths absolute: relative ones
// live in the data dir.
func (c *Config) resolve() {
	for _, p := range []*string{&c.Notify.Ntfy.HeaderFile, &c.Notify.Matrix.HeaderFile, &c.Notify.Webhook.CurlrcFile} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(c.DataDir, *p)
		}
	}
}

// Channels names the enabled notify channels in a fixed order.
func (c *Config) Channels() []string {
	var out []string
	if c.Notify.Ntfy.Enabled() {
		out = append(out, "ntfy")
	}
	if c.Notify.Matrix.Enabled() {
		out = append(out, "matrix")
	}
	if c.Notify.Webhook.Enabled() {
		out = append(out, "webhook")
	}
	return out
}

// KV is one line of the effective configuration for display.
type KV struct {
	Key   string
	Value string
}

// Public lists the effective configuration as the settings page
// shows it: every key, no file contents. The paths of the
// secret files appear; what is in them never does.
func (c *Config) Public() []KV {
	b := strconv.FormatBool
	i := strconv.Itoa
	return []KV{
		{"listen", c.Listen},
		{"base_url", c.BaseURL},
		{"data_dir", c.DataDir},
		{"metrics_public", b(c.MetricsPublic)},
		{"retention.sessions_days", i(c.Retention.SessionsDays)},
		{"retention.guard_days", i(c.Retention.GuardDays)},
		{"retention.versions_years", i(c.Retention.VersionsYears) + " (file versions: not in phase 1)"},
		{"eol.enabled", b(c.EOL.Enabled)},
		{"summary.weekday", c.Summary.Weekday},
		{"summary.hour", i(c.Summary.Hour)},
		{"notify.ntfy.server", c.Notify.Ntfy.Server},
		{"notify.ntfy.topic", c.Notify.Ntfy.Topic},
		{"notify.ntfy.header_file", c.Notify.Ntfy.HeaderFile},
		{"notify.matrix.server", c.Notify.Matrix.Server},
		{"notify.matrix.room", c.Notify.Matrix.Room},
		{"notify.matrix.header_file", c.Notify.Matrix.HeaderFile},
		{"notify.webhook.curlrc_file", c.Notify.Webhook.CurlrcFile},
	}
}
