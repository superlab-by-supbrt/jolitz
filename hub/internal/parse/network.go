package parse

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Link is one "- Link: a -> b (label)" line of network.md.
type Link struct {
	Src   string `json:"from"`
	Dst   string `json:"to"`
	Label string `json:"label"`
}

var linkLine = regexp.MustCompile(`^- Link:[ \t]*[^ \t]+[ \t]*->[ \t]*[^ \t]`)

// Network reads the link lines of network.md; the prose around
// them is left alone.
func Network(b []byte) []Link {
	var out []Link
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, "\r")
		if !linkLine.MatchString(line) {
			continue
		}
		l := strings.TrimSpace(line[7:])
		label := ""
		if strings.HasSuffix(l, ")") {
			if i := strings.Index(l, "("); i >= 0 {
				label = strings.TrimSuffix(l[i+1:], ")")
				l = l[:i]
			}
		}
		i := strings.Index(l, "->")
		a := strings.TrimRight(l[:i], " \t")
		b := strings.TrimSpace(l[i+2:])
		if a == "" || b == "" {
			continue
		}
		out = append(out, Link{Src: a, Dst: b, Label: strings.ReplaceAll(label, "\t", " ")})
	}
	return out
}

// AuditValue is one probed setting of a fleet audit snapshot.
type AuditValue struct {
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Audit reads a memory/audits/<stamp>.json snapshot: its date
// and every host/key/value, sorted by host and key. Values that
// are not strings are rendered as JSON text, as jq's tostring
// does in bin/jolitz-overview.
func Audit(b []byte) (string, []AuditValue, error) {
	var snap struct {
		Date  string                                `json:"date"`
		Hosts map[string]map[string]json.RawMessage `json:"hosts"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return "", nil, fmt.Errorf("audit snapshot: %w", err)
	}
	var out []AuditValue
	for host, kv := range snap.Hosts {
		for k, raw := range kv {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				s = string(raw)
			}
			out = append(out, AuditValue{Host: host, Key: k, Value: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Key < out[j].Key
	})
	return snap.Date, out, nil
}
