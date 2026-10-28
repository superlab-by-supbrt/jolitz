package parse

import (
	"regexp"
	"strings"
)

// Change is one entry of changelog.log (rules/changelog.md).
type Change struct {
	Date     string   `json:"date"`
	Time     string   `json:"time"`
	Host     string   `json:"host"`
	Who      string   `json:"who"`
	Headline string   `json:"headline"`
	Rollback string   `json:"rollback"`
	Flags    string   `json:"flags"`
	Detail   []string `json:"detail"`
}

var (
	entryStart = regexp.MustCompile(`^\[[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}\]`)
	labelLine  = regexp.MustCompile(`^[A-Z][a-z]+:`)
)

// Changelog parses a changelog.log for host. An entry starts
// with "[YYYY-MM-DD HH:MM]" in column 1; indented lines are its
// detail, with "Rollback:" and "Flags:" collected separately;
// a non-indented line before the first indented one continues
// the headline. Lines before the first entry are dropped.
func Changelog(host string, b []byte) []Change {
	var out []Change
	var cur *Change
	label := ""
	indet := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, "\r")
		if entryStart.MatchString(line) {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &Change{Date: line[1:11], Time: line[12:17], Host: host}
			rest := strings.TrimLeft(line[18:], " \t")
			if strings.HasPrefix(rest, "[") {
				if i := strings.Index(rest, "]"); i > 0 {
					cur.Who = rest[1:i]
					rest = strings.TrimLeft(rest[i+1:], " \t")
				}
			}
			cur.Headline = rest
			indet = false
			label = ""
			continue
		}
		if cur == nil || strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			indet = true
			l := strings.TrimLeft(line, " \t")
			cur.Detail = append(cur.Detail, l)
			switch {
			case strings.HasPrefix(l, "Rollback:"):
				label = "rb"
				l = l[9:]
			case strings.HasPrefix(l, "Flags:"):
				label = "fl"
				l = l[6:]
			case labelLine.MatchString(l):
				label = ""
				continue
			}
			l = strings.TrimLeft(l, " \t")
			switch label {
			case "rb":
				cur.Rollback = join(cur.Rollback, l)
			case "fl":
				cur.Flags = join(cur.Flags, l)
			}
			continue
		}
		if !indet {
			cur.Headline = join(cur.Headline, line)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	for i := range out {
		out[i].Headline = strings.ReplaceAll(out[i].Headline, "\t", " ")
		out[i].Rollback = strings.ReplaceAll(out[i].Rollback, "\t", " ")
		out[i].Flags = strings.ReplaceAll(out[i].Flags, "\t", " ")
	}
	return out
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}
