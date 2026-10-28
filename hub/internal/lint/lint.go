// Package lint finds values that look like credentials in a
// memory file. It is the TOKENS and ASSIGN check of
// bin/jolitz-memory-lint ported to Go: the same regular
// expressions, so a file the client lint passes is accepted by
// the hub and a file it refuses is refused here as well. The
// pattern list is a backstop, not a proof: a secret in an unknown
// format passes. The matching value is never returned, only the
// line number and the reason.
package lint

import (
	"bufio"
	"bytes"
	"regexp"
)

// Hit names the first line that looks like a secret.
type Hit struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Reason is the text of every hit, the wording of
// bin/jolitz-memory-lint.
const Reason = "looks like a secret value; record its location, not the value (rules/secrets.md)"

// b is the boundary before a token: start of line or a character
// outside the word set.
const b = `(^|[^A-Za-z0-9_])`

// tokens is TOKENS of bin/jolitz-memory-lint, case-sensitive.
var tokens = regexp.MustCompile(
	`-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----` +
		`|` + b + `gh[pousr]_[A-Za-z0-9]{36}` +
		`|` + b + `github_pat_[A-Za-z0-9_]{22,}` +
		`|` + b + `glpat-[A-Za-z0-9_-]{20}` +
		`|` + b + `xox[abprs]-[A-Za-z0-9-]{10,}` +
		`|` + b + `AKIA[0-9A-Z]{16}` +
		`|` + b + `sk-(ant-|proj-)?[A-Za-z0-9_-]{32,}` +
		`|` + b + `sk_live_[A-Za-z0-9]{20,}` +
		`|[A-Za-z][A-Za-z0-9+.-]*://[^/[:space:]:@]+:[^/[:space:]@$<{]+@` +
		`|\$(1|2[abxy]|5|6)\$[./A-Za-z0-9]+\$[./A-Za-z0-9]{20,}` +
		`|\$y\$[./A-Za-z0-9]+\$[./A-Za-z0-9]+\$[./A-Za-z0-9]{20,}`)

// assign is ASSIGN of bin/jolitz-memory-lint, case-insensitive:
// a credential name, "=" and a literal value. "Password: in
// /root/.my.cnf" has a colon and stays allowed.
var assign = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(password|passwd|passphrase|secret|token|api[_-]?key)[[:space:]]*=[[:space:]]*[^[:space:]$<{"']`)

// Check returns the first line of body that looks like a secret,
// or nil.
func Check(body []byte) *Hit {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Bytes()
		if tokens.Match(line) || assign.Match(line) {
			return &Hit{Line: n, Reason: Reason}
		}
	}
	return nil
}
