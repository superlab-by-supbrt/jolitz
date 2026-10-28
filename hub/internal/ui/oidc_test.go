package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/auth"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/auth/authtest"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
)

type oidcEnv struct {
	t   *testing.T
	st  *store.Store
	iss *authtest.Issuer
	srv *httptest.Server
	c   *http.Client
}

func newOIDCEnv(t *testing.T) *oidcEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddUser("alice", store.RoleOperator); err != nil {
		t.Fatal(err)
	}
	iss := authtest.New(t)
	e := &oidcEnv{t: t, st: st, iss: iss}
	mux := http.NewServeMux()
	e.srv = httptest.NewServer(mux)
	p := auth.New(iss.URL, iss.ClientID, iss.ClientSecret, e.srv.URL+"/oidc/callback", "preferred_username")
	mux.Handle("/", New(Config{Store: st, Version: "test", BaseURL: e.srv.URL, OIDC: p}))
	t.Cleanup(func() { e.srv.Close(); st.Close() })
	e.c = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return e
}

func (e *oidcEnv) do(method, path string, cookies ...*http.Cookie) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func cookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// start follows "log in with OIDC" up to the issuer and lets the
// issuer authorize a user with the given claims.
func (e *oidcEnv) start(claims map[string]any) (code, state string, flow *http.Cookie) {
	e.t.Helper()
	res, _ := e.do("GET", "/oidc/login")
	if res.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("/oidc/login: %d", res.StatusCode)
	}
	flow = cookie(res, flowCookie)
	if flow == nil || !flow.HttpOnly || flow.Path != "/oidc/" || flow.MaxAge <= 0 {
		e.t.Fatalf("flow cookie %+v", flow)
	}
	code, state = e.iss.Authorize(e.t, res.Header.Get("Location"), claims)
	return code, state, flow
}

func (e *oidcEnv) callback(code, state string, flow *http.Cookie) (*http.Response, string) {
	e.t.Helper()
	q := url.Values{"code": {code}, "state": {state}}
	if flow == nil {
		return e.do("GET", "/oidc/callback?"+q.Encode())
	}
	return e.do("GET", "/oidc/callback?"+q.Encode(), flow)
}

func TestOIDCLoginPageLink(t *testing.T) {
	e := newOIDCEnv(t)
	_, body := e.do("GET", "/login")
	if !strings.Contains(body, `href="/oidc/login"`) {
		t.Error("login page without the OIDC link")
	}
	// Without OIDC the page has no such link.
	plain := newEnv(t)
	_, body = plain.get("", "/login")
	if strings.Contains(body, "/oidc/login") {
		t.Error("OIDC link without OIDC")
	}
	if res, _ := plain.get("", "/oidc/login"); res.StatusCode != http.StatusNotFound {
		t.Errorf("/oidc/login without OIDC: %d", res.StatusCode)
	}
}

func TestOIDCHappyPath(t *testing.T) {
	e := newOIDCEnv(t)
	code, state, flow := e.start(map[string]any{"preferred_username": "alice"})
	res, _ := e.callback(code, state, flow)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	sess := cookie(res, api.CookieName)
	if sess == nil || !sess.HttpOnly || sess.Value == "" || sess.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie %+v", sess)
	}
	if gone := cookie(res, flowCookie); gone == nil || gone.MaxAge >= 0 {
		t.Errorf("flow cookie not cleared: %+v", gone)
	}
	res, body := e.do("GET", "/", sess)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "alice") {
		t.Errorf("fleet with the OIDC session: %d", res.StatusCode)
	}
	// The browser session is no bearer token for the API.
	req, _ := http.NewRequest("GET", e.srv.URL+"/login", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Value)
	if u, _ := api.Authenticate(e.st, req); u != nil {
		t.Error("an OIDC session secret works as a bearer token")
	}
	// The code is spent: a replay fails.
	if res, _ := e.callback(code, state, flow); res.StatusCode == http.StatusSeeOther {
		t.Error("a replayed callback logged in")
	}
	// Logout ends the session on the server too.
	req, _ = http.NewRequest("POST", e.srv.URL+"/logout", nil)
	req.AddCookie(sess)
	if _, err := e.c.Do(req); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.do("GET", "/", sess); res.StatusCode != http.StatusSeeOther {
		t.Errorf("session survives logout: %d", res.StatusCode)
	}
}

func TestOIDCUnknownUser(t *testing.T) {
	e := newOIDCEnv(t)
	code, state, flow := e.start(map[string]any{"preferred_username": "mallory<script>"})
	res, body := e.callback(code, state, flow)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown user: %d", res.StatusCode)
	}
	if cookie(res, api.CookieName) != nil {
		t.Error("an unknown user got a session cookie")
	}
	if !strings.Contains(body, "mallory&lt;script&gt;") || strings.Contains(body, "mallory<script>") {
		t.Error("the 403 page does not name the user, escaped")
	}
	if !strings.Contains(body, "jolitz-hub user add") {
		t.Error("the 403 page does not say how the user is created")
	}
	if users, _ := e.st.ListUsers(); len(users) != 1 {
		t.Errorf("OIDC created a user: %+v", users)
	}
}

func TestOIDCRejections(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(map[string]any)
		kid    string
	}{
		"wrong aud":   {mutate: func(m map[string]any) { m["aud"] = "other-client" }},
		"expired":     {mutate: func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() }},
		"wrong nonce": {mutate: func(m map[string]any) { m["nonce"] = "not-this-login" }},
		"unknown kid": {kid: "rsa-9"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newOIDCEnv(t)
			e.iss.Mutate = c.mutate
			if c.kid != "" {
				e.iss.KID = c.kid
			}
			code, state, flow := e.start(map[string]any{"preferred_username": "alice"})
			res, body := e.callback(code, state, flow)
			if res.StatusCode != http.StatusUnauthorized || cookie(res, api.CookieName) != nil {
				t.Errorf("%d, cookie %v", res.StatusCode, cookie(res, api.CookieName))
			}
			if !strings.Contains(body, "did not verify") {
				t.Errorf("page: %s", body)
			}
		})
	}
}

func TestOIDCStateChecks(t *testing.T) {
	e := newOIDCEnv(t)
	code, state, flow := e.start(map[string]any{"preferred_username": "alice"})
	if res, _ := e.callback(code, state, nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("no flow cookie: %d", res.StatusCode)
	}
	if res, _ := e.callback(code, state+"x", flow); res.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong state: %d", res.StatusCode)
	}
	// The issuer reports an error instead of a code.
	res, body := e.do("GET", "/oidc/callback?error=access_denied&state="+url.QueryEscape(state), flow)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "access_denied") {
		t.Errorf("issuer error: %d %s", res.StatusCode, body)
	}
}
