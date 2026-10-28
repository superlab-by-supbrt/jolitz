// Package parse reads the files of a jolitz memory/ tree the way
// bin/jolitz-overview does: memory.md facts, changelog.log
// entries, todo.md items, "- Link:" lines of network.md and the
// fleet audit snapshots. It is a port of the awk programs in
// bin/jolitz-overview and lib/overview.awk; where the two could
// differ, the awk is the reference.
package parse

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// Service is one entry of "- Services: nginx 1.26, docker".
type Service struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Host holds the facts of one memory.md.
type Host struct {
	Heading       string
	OS            string
	IP            string
	Distro        string
	Tags          []string
	SSHPort       string
	Mode          string
	Owner         string
	Services      []Service
	Guest         []string
	LastConnected string
	// Product and Version name the OS for endoflife.date, as
	// bin/jolitz-overview derives them from the OS line.
	Product string
	Version string
	// Fields holds every top-level "- Key: value" line, first
	// occurrence wins.
	Fields map[string]string
}

var factLine = regexp.MustCompile(`^- ([A-Za-z][A-Za-z ]*):`)

// Memory parses a memory.md.
func Memory(b []byte) Host {
	h := Host{Fields: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if h.Heading == "" && strings.HasPrefix(line, "# ") {
			h.Heading = strings.TrimSpace(line[2:])
			continue
		}
		m := factLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		k := m[1]
		v := strings.TrimSpace(line[len(m[0]):])
		v = strings.ReplaceAll(v, "\t", " ")
		if _, seen := h.Fields[k]; !seen {
			h.Fields[k] = v
		}
	}
	h.OS = h.Fields["OS"]
	h.IP = h.Fields["IP"]
	h.Distro = h.Fields["Distro family"]
	h.SSHPort = h.Fields["SSH port"]
	h.Mode = h.Fields["Mode"]
	h.Owner = h.Fields["Owner"]
	h.Tags = SplitList(h.Fields["Tags"])
	h.Guest = SplitList(h.Fields["Guest"])
	h.Services = Services(h.Fields["Services"])
	if lc := strings.Fields(h.Fields["Last connected"]); len(lc) > 0 {
		h.LastConnected = lc[0]
	}
	h.Product = product(h.OS)
	h.Version = version(h.OS, h.Product)
	return h
}

// SplitList turns "prod, Web" into ["prod", "Web"]: commas and
// whitespace separate, empty items drop.
func SplitList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	}) {
		out = append(out, f)
	}
	return out
}

// Services splits "nginx 1.26, docker" into name and version pairs.
func Services(s string) []Service {
	var out []string
	if s != "" {
		out = strings.Split(s, ",")
	}
	var svc []Service
	for _, v := range out {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		name, ver, _ := strings.Cut(v, " ")
		svc = append(svc, Service{Name: name, Version: strings.TrimLeft(ver, " \t")})
	}
	return svc
}

var productRules = []struct{ pat, product string }{
	{"proxmox", "proxmox-ve"},
	{"opnsense", "opnsense"},
	{"centos stream", "centos-stream"},
	{"rocky", "rocky-linux"},
	{"alma", "almalinux"},
	{"red hat|rhel", "rhel"},
	{"fedora", "fedora"},
	{"suse linux enterprise|sles", "sles"},
	{"opensuse leap", "opensuse"},
	{"ubuntu", "ubuntu"},
	{"debian", "debian"},
	{"freebsd", "freebsd"},
	{"netbsd", "netbsd"},
	{"mac ?os", "macos"},
	{"alpine", "alpine-linux"},
}

var productRE = func() []*regexp.Regexp {
	rs := make([]*regexp.Regexp, len(productRules))
	for i, r := range productRules {
		rs[i] = regexp.MustCompile(r.pat)
	}
	return rs
}()

func product(os string) string {
	l := strings.ToLower(os)
	for i, r := range productRules {
		if productRE[i].MatchString(l) {
			return r.product
		}
	}
	return ""
}

var (
	versionRE = regexp.MustCompile(`[0-9]+(\.[0-9]+)*`)
	spRE      = regexp.MustCompile(`sp[0-9]+`)
)

func version(os, p string) string {
	l := strings.ToLower(os)
	v := versionRE.FindString(l)
	if v == "" {
		return ""
	}
	if p == "sles" {
		if sp := spRE.FindString(l); sp != "" {
			v, _, _ = strings.Cut(v, ".")
			v = v + "." + sp[2:]
		}
	}
	return v
}

// Todo is one "- [ ]" or "- [x]" line of todo.md.
type Todo struct {
	Text string `json:"text"`
	Open bool   `json:"open"`
}

var todoLine = regexp.MustCompile(`^[ \t]*- \[([ xX])\][ \t]*(.*)$`)

// Todos parses a todo.md.
func Todos(b []byte) []Todo {
	var out []Todo
	for _, line := range strings.Split(string(b), "\n") {
		m := todoLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out = append(out, Todo{Text: strings.ReplaceAll(m[2], "\t", " "), Open: m[1] == " "})
	}
	return out
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

// ValidHost reports whether s is a host name the hub accepts in a
// path: letters, digits, dots and dashes, starting with a letter
// or digit. The same set bin/jolitz-overview accepts.
func ValidHost(s string) bool {
	return hostRE.MatchString(s)
}

// Alias reads an alias file (rules/dns-aliases.md, the Windows
// form): its first line is the canonical host name. It returns
// "" when the body is not a single host name.
func Alias(b []byte) string {
	s := strings.TrimRight(string(b), "\r\n \t")
	if strings.ContainsAny(s, "\r\n") {
		return ""
	}
	s = strings.TrimSpace(s)
	if !ValidHost(s) {
		return ""
	}
	return s
}
