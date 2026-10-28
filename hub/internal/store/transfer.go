package store

import (
	"archive/tar"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ExportFormat is the version of the export layout below.
const ExportFormat = 1

// The export is a tar archive, the way a hub moves between
// backends (jolitz-hub export, then import into an empty
// database):
//
//	manifest.json   {format, schema, backend, hub_version,
//	                 exported_at, files: [{path, version,
//	                 sha256}], counts}
//	users.json      [{name, role, token_sha256, created_at,
//	                 oidc_issuer, oidc_subject}]: the two oidc
//	                 fields only for a user bound to an OIDC
//	                 identity; an entry without them is unbound
//	claims.json     [{host, user, note, since}]
//	history.json    [{path, version, sha256, size, updated_at,
//	                 updated_by}]: every version of every path,
//	                 deleted paths included, by path and version
//	bodies/<sha256> the bytes of each distinct body, once
//	memory/<path>   the current files, as GET /api/v1/export
//	                 gives them; import ignores them
//
// Times are RFC 3339 text in UTC, as both backends store them.
// The derived tables (hosts, changes, todos, links, audits,
// aliases) are not in it: import rebuilds them from the files.
// Sessions, guard denials, browser logins, the endoflife.date
// cache and the job runs stay behind.

// ErrNotEmpty: import needs an empty database.
var ErrNotEmpty = errors.New("the database is not empty")

type exportManifest struct {
	Format     int            `json:"format"`
	Schema     int            `json:"schema"`
	Backend    string         `json:"backend"`
	HubVersion string         `json:"hub_version"`
	ExportedAt string         `json:"exported_at"`
	Files      []exportFile   `json:"files"`
	Counts     map[string]int `json:"counts"`
}

type exportFile struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

type exportUser struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	TokenSHA256 string `json:"token_sha256"`
	CreatedAt   string `json:"created_at"`
	OIDCIssuer  string `json:"oidc_issuer,omitempty"`
	OIDCSubject string `json:"oidc_subject,omitempty"`
}

type exportClaim struct {
	Host  string `json:"host"`
	User  string `json:"user"`
	Note  string `json:"note"`
	Since string `json:"since"`
}

type exportVersion struct {
	Path      string `json:"path"`
	Version   int    `json:"version"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// ImportCounts says what an import stored.
type ImportCounts struct {
	Files    int `json:"files"`
	Versions int `json:"versions"`
	Users    int `json:"users"`
	Claims   int `json:"claims"`
}

// beginRead opens a transaction that sees one state of the
// database from its first to its last read.
func (s *Store) beginRead() (*txn, error) {
	opts := &sql.TxOptions{ReadOnly: true}
	if s.db.pg {
		opts.Isolation = sql.LevelRepeatableRead
	} else {
		opts = nil // SQLite: a transaction is a snapshot already
	}
	t, err := s.db.DB.BeginTx(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	return &txn{Tx: t, pg: s.db.pg}, nil
}

// Export writes the archive described above. hubVersion lands
// in the manifest.
func (s *Store) Export(w io.Writer, hubVersion string) error {
	t, err := s.beginRead()
	if err != nil {
		return err
	}
	defer t.Rollback()

	scan := func(q string, fn func(scanner) error) error {
		rows, err := t.Query(q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	users := []exportUser{}
	if err := scan(`SELECT name, role, token_sha256, created_at, oidc_issuer, oidc_subject FROM users ORDER BY name`, func(sc scanner) error {
		var u exportUser
		var iss, sub sql.NullString
		if err := sc.Scan(&u.Name, &u.Role, &u.TokenSHA256, &u.CreatedAt, &iss, &sub); err != nil {
			return err
		}
		if iss.Valid && sub.Valid {
			u.OIDCIssuer, u.OIDCSubject = iss.String, sub.String
		}
		users = append(users, u)
		return nil
	}); err != nil {
		return err
	}
	claims := []exportClaim{}
	if err := scan(`SELECT host, "user", note, since FROM claims ORDER BY host`, func(sc scanner) error {
		var c exportClaim
		if err := sc.Scan(&c.Host, &c.User, &c.Note, &c.Since); err != nil {
			return err
		}
		claims = append(claims, c)
		return nil
	}); err != nil {
		return err
	}
	history := []exportVersion{}
	bodies := map[string][]byte{}
	if err := scan(`SELECT path, version, sha256, size, updated_at, updated_by, body FROM file_versions ORDER BY path, version`, func(sc scanner) error {
		var v exportVersion
		var body []byte
		if err := sc.Scan(&v.Path, &v.Version, &v.SHA256, &v.Size, &v.UpdatedAt, &v.UpdatedBy, &body); err != nil {
			return err
		}
		if SHA256(body) != v.SHA256 {
			return fmt.Errorf("%s version %d: stored body does not match its sha256", v.Path, v.Version)
		}
		history = append(history, v)
		bodies[v.SHA256] = body
		return nil
	}); err != nil {
		return err
	}
	files := []exportFile{}
	if err := scan(`SELECT path, version, sha256 FROM files ORDER BY path`, func(sc scanner) error {
		var f exportFile
		if err := sc.Scan(&f.Path, &f.Version, &f.SHA256); err != nil {
			return err
		}
		if _, ok := bodies[f.SHA256]; !ok {
			return fmt.Errorf("%s: version %d is missing from the history", f.Path, f.Version)
		}
		files = append(files, f)
		return nil
	}); err != nil {
		return err
	}
	ver, err := s.Version()
	if err != nil {
		return err
	}
	m := exportManifest{
		Format: ExportFormat, Schema: ver, Backend: s.Backend(), HubVersion: hubVersion,
		ExportedAt: s.now(), Files: files,
		Counts: map[string]int{"files": len(files), "versions": len(history), "bodies": len(bodies),
			"users": len(users), "claims": len(claims)},
	}

	tw := tar.NewWriter(w)
	mtime := s.Now().UTC().Truncate(time.Second)
	put := func(name string, b []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)),
			ModTime: mtime, Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}
	putJSON := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		return put(name, append(b, '\n'))
	}
	for _, e := range []struct {
		name string
		v    any
	}{{"manifest.json", m}, {"users.json", users}, {"claims.json", claims}, {"history.json", history}} {
		if err := putJSON(e.name, e.v); err != nil {
			return err
		}
	}
	shas := make([]string, 0, len(bodies))
	for sha := range bodies {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	for _, sha := range shas {
		if err := put("bodies/"+sha, bodies[sha]); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := put("memory/"+f.Path, bodies[f.SHA256]); err != nil {
			return err
		}
	}
	return tw.Close()
}

// Empty reports whether the database holds no user, file,
// history entry or claim.
func (s *Store) Empty() (bool, error) {
	return empty(s.db)
}

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func empty(q rowQuerier) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM files)
		+ (SELECT count(*) FROM file_versions) + (SELECT count(*) FROM claims)`).Scan(&n)
	return n == 0, err
}

// maxImportEntry bounds one entry of an archive; memory files
// are small, and the API takes no body over MaxBody either.
const maxImportEntry = 64 << 20

// Import reads an archive written by Export into an empty
// database, in one transaction: either everything is stored or
// nothing. Every body is checked against its sha256, every path
// and user against the rules PutFile and AddUser apply; the
// derived tables are rebuilt from the current files.
func (s *Store) Import(r io.Reader) (*ImportCounts, error) {
	ents := map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		known := h.Name == "manifest.json" || h.Name == "users.json" || h.Name == "claims.json" ||
			h.Name == "history.json" || strings.HasPrefix(h.Name, "bodies/") || strings.HasPrefix(h.Name, "memory/")
		if !known || h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive: unexpected entry %q", h.Name)
		}
		if strings.HasPrefix(h.Name, "memory/") {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxImportEntry+1))
		if err != nil {
			return nil, fmt.Errorf("archive: %s: %w", h.Name, err)
		}
		if len(b) > maxImportEntry {
			return nil, fmt.Errorf("archive: %s is larger than %d bytes", h.Name, maxImportEntry)
		}
		ents[h.Name] = b
	}
	var m exportManifest
	var users []exportUser
	var claims []exportClaim
	var history []exportVersion
	for name, v := range map[string]any{"manifest.json": &m, "users.json": &users, "claims.json": &claims, "history.json": &history} {
		b, ok := ents[name]
		if !ok {
			return nil, fmt.Errorf("archive: %s missing; not an export of jolitz-hub", name)
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("archive: %s: %w", name, err)
		}
	}
	if m.Format != ExportFormat {
		return nil, fmt.Errorf("archive: format %d; this hub reads format %d", m.Format, ExportFormat)
	}

	// Validate everything before the first write.
	checkTime := func(what, v string) error {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return fmt.Errorf("archive: %s: time %q is not RFC 3339", what, v)
		}
		return nil
	}
	bound := map[string]string{}
	for _, u := range users {
		if !userName.MatchString(u.Name) || rank(u.Role) == 0 || len(u.TokenSHA256) != 64 {
			return nil, fmt.Errorf("%w: user %q (role %q)", ErrInvalid, u.Name, u.Role)
		}
		if err := checkTime("user "+u.Name, u.CreatedAt); err != nil {
			return nil, err
		}
		if (u.OIDCIssuer == "") != (u.OIDCSubject == "") {
			return nil, fmt.Errorf("%w: user %q: oidc_issuer and oidc_subject go together", ErrInvalid, u.Name)
		}
		if u.OIDCIssuer != "" {
			key := u.OIDCIssuer + "\x00" + u.OIDCSubject
			if other, dup := bound[key]; dup {
				return nil, fmt.Errorf("%w: users %q and %q are bound to one oidc identity", ErrInvalid, other, u.Name)
			}
			bound[key] = u.Name
		}
	}
	for _, c := range claims {
		if err := checkTime("claim "+c.Host, c.Since); err != nil {
			return nil, err
		}
	}
	byVersion := map[string]exportVersion{}
	for _, v := range history {
		if kind, _ := Classify(v.Path); kind == KindInvalid || v.Version < 1 {
			return nil, fmt.Errorf("%w: history entry %q version %d", ErrInvalid, v.Path, v.Version)
		}
		body, ok := ents["bodies/"+v.SHA256]
		if !ok {
			return nil, fmt.Errorf("archive: body %s of %s version %d missing", v.SHA256, v.Path, v.Version)
		}
		if SHA256(body) != v.SHA256 || int64(len(body)) != v.Size {
			return nil, fmt.Errorf("archive: body of %s version %d does not match its sha256 and size", v.Path, v.Version)
		}
		if err := checkTime(v.Path, v.UpdatedAt); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s\x00%d", v.Path, v.Version)
		if _, dup := byVersion[key]; dup {
			return nil, fmt.Errorf("archive: %s version %d twice", v.Path, v.Version)
		}
		byVersion[key] = v
	}
	type current struct {
		v    exportVersion
		kind Kind
		host string
		body []byte
	}
	var cur []current
	for _, f := range m.Files {
		v, ok := byVersion[fmt.Sprintf("%s\x00%d", f.Path, f.Version)]
		if !ok || v.SHA256 != f.SHA256 {
			return nil, fmt.Errorf("archive: current file %s version %d is not in the history", f.Path, f.Version)
		}
		body := ents["bodies/"+v.SHA256]
		kind, host, err := checkPath(f.Path, body)
		if err != nil {
			return nil, err
		}
		cur = append(cur, current{v: v, kind: kind, host: host, body: body})
	}
	sort.Slice(cur, func(i, j int) bool { return cur[i].v.Path < cur[j].v.Path })

	t, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	ok, err := empty(t)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotEmpty
	}
	for _, u := range users {
		var iss, sub any // NULL for an unbound user
		if u.OIDCIssuer != "" {
			iss, sub = u.OIDCIssuer, u.OIDCSubject
		}
		if _, err := t.Exec(`INSERT INTO users(name, token_sha256, role, created_at, oidc_issuer, oidc_subject) VALUES(?,?,?,?,?,?)`,
			u.Name, u.TokenSHA256, u.Role, u.CreatedAt, iss, sub); err != nil {
			return nil, fmt.Errorf("user %s: %w", u.Name, err)
		}
	}
	for _, v := range history {
		if _, err := t.Exec(`INSERT INTO file_versions(path, version, body, sha256, size, updated_at, updated_by) VALUES(?,?,?,?,?,?,?)`,
			v.Path, v.Version, ents["bodies/"+v.SHA256], v.SHA256, v.Size, v.UpdatedAt, v.UpdatedBy); err != nil {
			return nil, fmt.Errorf("%s version %d: %w", v.Path, v.Version, err)
		}
	}
	now := s.now()
	for _, c := range cur {
		v := c.v
		if _, err := t.Exec(`INSERT INTO files(path, body, sha256, size, updated_at, updated_by, version) VALUES(?,?,?,?,?,?,?)`,
			v.Path, c.body, v.SHA256, v.Size, v.UpdatedAt, v.UpdatedBy, v.Version); err != nil {
			return nil, fmt.Errorf("%s: %w", v.Path, err)
		}
		if err := derive(t, c.kind, c.host, c.body, now); err != nil {
			return nil, fmt.Errorf("%s: %w", v.Path, err)
		}
	}
	for _, c := range claims {
		if _, err := t.Exec(`INSERT INTO claims(host, "user", note, since) VALUES(?,?,?,?)`, c.Host, c.User, c.Note, c.Since); err != nil {
			return nil, fmt.Errorf("claim %s: %w", c.Host, err)
		}
	}
	if err := t.Commit(); err != nil {
		return nil, err
	}
	return &ImportCounts{Files: len(cur), Versions: len(history), Users: len(users), Claims: len(claims)}, nil
}
