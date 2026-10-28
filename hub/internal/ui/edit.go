package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/lint"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/parse"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// The edits the UI allows: to-dos, owner and tags of a host,
// links of the network. Each one rewrites the memory file the
// value lives in, so the change is a new file version by the UI
// user, passes the same lint as a push and lands in the next
// pull. Nothing else is written from the browser.

// editError carries the HTTP status an edit failed with.
type editError struct {
	code int
	msg  string
}

func (e *editError) Error() string { return e.msg }

func bad(msg string) error { return &editError{http.StatusBadRequest, msg} }

// linted runs the push lint over a new body; a hit becomes a
// 422 that names the line, never the value.
func linted(body []byte) ([]byte, error) {
	if hit := lint.Check(body); hit != nil {
		return nil, &editError{http.StatusUnprocessableEntity, fmt.Sprintf("Refused: line %d looks like a secret value (%s). A credential's place in memory is its location, not its value.", hit.Line, hit.Reason)}
	}
	return body, nil
}

// editFile applies edit to a file and answers the request: a
// redirect on success, an error page otherwise. unchanged is
// returned by edit when nothing is to write.
var unchanged = errors.New("unchanged")

func (s *server) editFile(w http.ResponseWriter, r *http.Request, u *store.User, path, back string, edit func(old []byte) ([]byte, error)) {
	_, err := s.cfg.Store.UpdateFile(path, u.Name, edit)
	var ee *editError
	switch {
	case errors.Is(err, unchanged):
	case errors.As(err, &ee):
		s.errorPage(w, r, ee.code, ee.msg)
		return
	case errors.Is(err, store.ErrMismatch):
		s.errorPage(w, r, http.StatusConflict, "Someone wrote the file at the same moment; reload and try again.")
		return
	case errors.Is(err, store.ErrInvalid):
		s.errorPage(w, r, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// edit is the common start of every edit handler: operator role,
// a small form.
func (s *server) edit(w http.ResponseWriter, r *http.Request, u *store.User) bool {
	if !store.RoleAtLeast(u.Role, store.RoleOperator) {
		s.errorPage(w, r, http.StatusForbidden, "Edits need the operator role.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "Bad form.")
		return false
	}
	return true
}

// oneLine trims a form value and refuses newlines and more than
// max characters.
func oneLine(v string, max int) (string, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\t", " "))
	if strings.ContainsAny(v, "\r\n") {
		return "", bad("One line, please.")
	}
	if len(v) > max {
		return "", bad(fmt.Sprintf("At most %d characters.", max))
	}
	return v, nil
}

// knownHost says whether the hub has files of a host.
func (s *server) knownHost(w http.ResponseWriter, r *http.Request, name string) bool {
	_, err := s.cfg.Store.Host(name)
	if errors.Is(err, store.ErrNotFound) {
		s.errorPage(w, r, http.StatusNotFound, "No such host in the hub.")
		return false
	}
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	return true
}

// ---- to-dos ------------------------------------------------

var todoLine = regexp.MustCompile(`^([ \t]*- \[)([ xX])(\][ \t]*)(.*)$`)

// lines splits a body into lines without a trailing empty one.
func lines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinLines(ls []string) []byte {
	if len(ls) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(ls, "\n") + "\n")
}

// toggleTodo flips the first item with that text whose state
// differs from done. The second value says whether a line
// changed.
func toggleTodo(body []byte, text string, done bool) ([]byte, bool) {
	ls := lines(body)
	for i, l := range ls {
		m := todoLine.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil || strings.ReplaceAll(m[4], "\t", " ") != text {
			continue
		}
		if (m[2] != " ") == done {
			continue
		}
		mark := " "
		if done {
			mark = "x"
		}
		ls[i] = m[1] + mark + m[3] + m[4]
		return joinLines(ls), true
	}
	return body, false
}

// addTodo appends an open item; a missing file starts with the
// heading of rules/server-memory.md.
func addTodo(body []byte, text string) []byte {
	if len(body) == 0 {
		body = []byte("# To-do\n")
	} else if body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	return append(body, []byte("- [ ] "+text+"\n")...)
}

func (s *server) todos(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := s.hostName(w, r)
	if name == "" || !s.edit(w, r, u) || !s.knownHost(w, r, name) {
		return
	}
	text, err := oneLine(r.PostForm.Get("text"), 500)
	if err == nil && text == "" {
		err = bad("The to-do text is empty.")
	}
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, err.Error())
		return
	}
	path := "servers/" + name + "/todo.md"
	back := "/hosts/" + url.PathEscape(name)
	switch r.PostForm.Get("action") {
	case "toggle":
		done := r.PostForm.Get("done") == "1"
		s.editFile(w, r, u, path, back, func(old []byte) ([]byte, error) {
			body, changed := toggleTodo(old, text, done)
			if !changed {
				return nil, unchanged
			}
			return linted(body)
		})
	case "add":
		s.editFile(w, r, u, path, back, func(old []byte) ([]byte, error) {
			return linted(addTodo(old, text))
		})
	default:
		s.errorPage(w, r, http.StatusBadRequest, "action is toggle or add.")
	}
}

// ---- owner and tags ----------------------------------------

var factLine = regexp.MustCompile(`^- ([A-Za-z][A-Za-z ]*):`)

// setFact replaces the first "- Key:" line of a memory.md, or
// appends one at the end of the facts block after the heading.
// An empty value removes the line.
func setFact(body []byte, key, value string) []byte {
	ls := lines(body)
	prefix := "- " + key + ":"
	for i, l := range ls {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		if value == "" {
			return joinLines(append(ls[:i], ls[i+1:]...))
		}
		ls[i] = prefix + " " + value
		return joinLines(ls)
	}
	if value == "" {
		return body
	}
	// After the last fact of the first run of facts; else after
	// the heading; else at the end.
	at := len(ls)
	first := -1
	for i, l := range ls {
		if factLine.MatchString(l) {
			if first < 0 {
				first = i
			}
			at = i + 1
		} else if first >= 0 {
			break
		}
	}
	if first < 0 {
		at = len(ls)
		if len(ls) > 0 && strings.HasPrefix(ls[0], "# ") {
			at = 1
		}
	}
	out := make([]string, 0, len(ls)+1)
	out = append(out, ls[:at]...)
	out = append(out, prefix+" "+value)
	out = append(out, ls[at:]...)
	return joinLines(out)
}

func (s *server) facts(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := s.hostName(w, r)
	if name == "" || !s.edit(w, r, u) || !s.knownHost(w, r, name) {
		return
	}
	owner, err := oneLine(r.PostForm.Get("owner"), 200)
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "owner: "+err.Error())
		return
	}
	tags, err := oneLine(r.PostForm.Get("tags"), 200)
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "tags: "+err.Error())
		return
	}
	tags = strings.Join(parse.SplitList(tags), ", ")
	path := "servers/" + name + "/memory.md"
	s.editFile(w, r, u, path, "/hosts/"+url.PathEscape(name), func(old []byte) ([]byte, error) {
		if old == nil {
			return nil, &editError{http.StatusNotFound, "This host has no memory.md on the hub; jolitz writes it on the next connection."}
		}
		body := setFact(setFact(old, "Owner", owner), "Tags", tags)
		if string(body) == string(old) {
			return nil, unchanged
		}
		return linted(body)
	})
}

// ---- links -------------------------------------------------

// linkLine renders a "- Link:" line as parse.Network reads it.
func linkLine(l parse.Link) string {
	s := "- Link: " + l.Src + " -> " + l.Dst
	if l.Label != "" {
		s += " (" + l.Label + ")"
	}
	return s
}

// addLink appends a link line; a missing file starts with the
// heading.
func addLink(body []byte, l parse.Link) []byte {
	if len(body) == 0 {
		body = []byte("# Network\n")
	} else if body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	return append(body, []byte(linkLine(l)+"\n")...)
}

// removeLink drops the first line that parses to the link.
func removeLink(body []byte, l parse.Link) ([]byte, bool) {
	ls := lines(body)
	for i, line := range ls {
		got := parse.Network([]byte(line))
		if len(got) == 1 && got[0] == l {
			return joinLines(append(ls[:i], ls[i+1:]...)), true
		}
	}
	return body, false
}

func (s *server) links(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.edit(w, r, u) {
		return
	}
	l := parse.Link{Src: strings.TrimSpace(r.PostForm.Get("from")), Dst: strings.TrimSpace(r.PostForm.Get("to"))}
	if !parse.ValidHost(l.Src) || !parse.ValidHost(l.Dst) {
		s.errorPage(w, r, http.StatusBadRequest, "from and to are host names: letters, digits, dots and dashes.")
		return
	}
	label, err := oneLine(r.PostForm.Get("label"), 100)
	if err == nil && strings.ContainsAny(label, "()") {
		err = bad("No parentheses in a label.")
	}
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "label: "+err.Error())
		return
	}
	l.Label = label
	switch r.PostForm.Get("action") {
	case "add":
		s.editFile(w, r, u, "network.md", "/", func(old []byte) ([]byte, error) {
			return linted(addLink(old, l))
		})
	case "remove":
		s.editFile(w, r, u, "network.md", "/", func(old []byte) ([]byte, error) {
			body, changed := removeLink(old, l)
			if !changed {
				return nil, unchanged
			}
			return linted(body)
		})
	default:
		s.errorPage(w, r, http.StatusBadRequest, "action is add or remove.")
	}
}
