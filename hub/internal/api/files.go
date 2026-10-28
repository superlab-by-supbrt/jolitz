package api

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/lint"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

// The files endpoints: the manifest, one file (current or by
// version), its history, PUT with a mandatory If-Match once the
// file exists, DELETE and the export tar.

func (s *server) listFiles(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	files, err := s.cfg.Store.ListFiles()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

const pathHelp = "path outside the allowed set: servers/<host>/(memory.md|changelog.log|todo.md|rules.md), servers/<alias>, network.md, audits/<YYYY-MM-DD-HHMM>.json; host names are [A-Za-z0-9][A-Za-z0-9.-]*"

// filePath validates the path wildcard; "" with a 400 written.
func filePath(w http.ResponseWriter, r *http.Request) string {
	p := r.PathValue("path")
	if kind, _ := store.Classify(p); kind == store.KindInvalid {
		fail(w, http.StatusBadRequest, pathHelp)
		return ""
	}
	return p
}

// getFile serves GET files/{path}, files/{path}?version=N and
// files/{path}/history. The history suffix cannot collide with
// a file: no allowed path ends in /history.
func (s *server) getFile(w http.ResponseWriter, r *http.Request, u *store.User) {
	if p, ok := strings.CutSuffix(r.PathValue("path"), "/history"); ok {
		if kind, _ := store.Classify(p); kind != store.KindInvalid {
			s.history(w, p)
			return
		}
	}
	p := filePath(w, r)
	if p == "" {
		return
	}
	var body []byte
	var info *store.FileInfo
	var err error
	if v := r.URL.Query().Get("version"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 {
			fail(w, http.StatusBadRequest, "version must be a positive number")
			return
		}
		body, info, err = s.cfg.Store.GetFileVersion(p, n)
	} else {
		body, info, err = s.cfg.Store.GetFile(p)
	}
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such file or version")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+info.SHA256+`"`)
	w.Header().Set("X-Version", strconv.Itoa(info.Version))
	w.Header().Set("Last-Modified", info.UpdatedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *server) history(w http.ResponseWriter, p string) {
	hist, err := s.cfg.Store.FileHistory(p)
	if err != nil {
		internal(w, err)
		return
	}
	if len(hist) == 0 {
		fail(w, http.StatusNotFound, "no such file")
		return
	}
	writeJSON(w, http.StatusOK, hist)
}

// conflict is the body of a 409 (If-Match names another version
// than the stored one) and of a 428 (no If-Match on an existing
// file): the hub's version and sha256, the request's sha256, and
// a unified diff of the hub's body against the request body.
type conflict struct {
	Error      string `json:"error"`
	Version    int    `json:"version"`
	SHA256     string `json:"sha256"`
	YoursSHA   string `json:"yours_sha256"`
	Diff       string `json:"diff"`
	LinesAdded int    `json:"lines_added"`
	LinesGone  int    `json:"lines_removed"`
}

func (s *server) conflict(w http.ResponseWriter, code int, msg, p string, yours []byte) {
	cur, info, err := s.cfg.Store.GetFile(p)
	if errors.Is(err, store.ErrNotFound) {
		// If-Match on a file that is not there.
		c := conflict{Error: msg, YoursSHA: store.SHA256(yours), Diff: Unified("a/"+p+" (not on the hub)", "b/"+p+" (yours)", nil, yours)}
		c.LinesAdded, c.LinesGone = DiffStat(c.Diff)
		writeJSON(w, code, c)
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	c := conflict{Error: msg, Version: info.Version, SHA256: info.SHA256, YoursSHA: store.SHA256(yours),
		Diff: Unified(fmt.Sprintf("a/%s (hub, version %d, %s)", p, info.Version, info.UpdatedBy), "b/"+p+" (yours)", cur, yours)}
	c.LinesAdded, c.LinesGone = DiffStat(c.Diff)
	writeJSON(w, code, c)
}

func (s *server) putFile(w http.ResponseWriter, r *http.Request, u *store.User) {
	p := filePath(w, r)
	if p == "" {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}
	if hit := lint.Check(body); hit != nil {
		writeJSON(w, http.StatusUnprocessableEntity, hit)
		return
	}
	// If-Match: the sha256 the client built on. "*" overwrites
	// whatever is there; nothing at all is allowed only while
	// the file does not exist yet.
	ifMatch := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `" `)
	_, _, getErr := s.cfg.Store.GetFile(p)
	created := errors.Is(getErr, store.ErrNotFound)
	if !created && getErr != nil {
		internal(w, getErr)
		return
	}
	switch {
	case ifMatch == "*":
		ifMatch = ""
	case ifMatch == "" && created:
		ifMatch = "-"
	case ifMatch == "":
		s.conflict(w, http.StatusPreconditionRequired, "the file exists; send If-Match: <sha256> of the version you built on (pull first), or If-Match: * to overwrite", p, body)
		return
	}
	info, err := s.cfg.Store.PutFile(p, body, u.Name, ifMatch)
	switch {
	case errors.Is(err, store.ErrMismatch):
		s.conflict(w, http.StatusConflict, "If-Match does not match the stored sha256: someone pushed first; pull, merge, push again", p, body)
		return
	case errors.Is(err, store.ErrInvalid):
		fail(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		internal(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, info)
}

func (s *server) deleteFile(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := filePath(w, r)
	if p == "" {
		return
	}
	err := s.cfg.Store.DeleteFile(p)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such file")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Export writes every file as a tar archive with paths relative
// to memory/.
func Export(st *store.Store, w io.Writer) error {
	files, err := st.ListFiles()
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	for _, f := range files {
		body, info, err := st.GetFile(f.Path)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: f.Path, Mode: 0o644, Size: int64(len(body)), ModTime: info.UpdatedAt,
		}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	return tw.Close()
}

func (s *server) export(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	var buf bytes.Buffer
	if err := Export(s.cfg.Store, &buf); err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="jolitz-memory-%s.tar"`, s.cfg.Store.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
