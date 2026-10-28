package ui

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/auth"
)

// flowCookie carries state, nonce and PKCE verifier of one login
// attempt from /oidc/login to /oidc/callback. It is HttpOnly,
// limited to /oidc/ and ten minutes, and SameSite=Lax so that the
// issuer's top-level redirect back brings it along.
const flowCookie = "jolitz_hub_oidc"

const flowTTL = 10 * time.Minute

// WebSessionTTL is how long a browser session opened through
// OIDC lasts; then the issuer is asked again.
const WebSessionTTL = 12 * time.Hour

func (s *server) setFlowCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: value, Path: "/oidc/", HttpOnly: true, Secure: isTLS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

// oidcLogin starts an attempt and sends the browser to the
// issuer.
func (s *server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	l, err := auth.NewLogin()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	target, err := s.cfg.OIDC.AuthURL(r.Context(), l)
	if err != nil {
		log.Printf("ui: oidc: %v", err)
		s.errorPage(w, r, http.StatusBadGateway, "The identity provider cannot be reached; see the server log. Token login still works.")
		return
	}
	// The three values are base64url: no dot in them.
	s.setFlowCookie(w, r, l.State+"."+l.Nonce+"."+l.Verifier, int(flowTTL.Seconds()))
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// oidcCallback finishes an attempt: state against the flow
// cookie, the code exchanged with the PKCE verifier, the ID
// token verified, its username claim mapped to an existing
// user. Then the same login cookie as the token login, holding
// a fresh browser session.
func (s *server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var l auth.Login
	c, err := r.Cookie(flowCookie)
	if err == nil {
		parts := strings.Split(c.Value, ".")
		if len(parts) == 3 {
			l = auth.Login{State: parts[0], Nonce: parts[1], Verifier: parts[2]}
		}
	}
	s.setFlowCookie(w, r, "", -1)
	if l.State == "" || subtle.ConstantTimeCompare([]byte(l.State), []byte(q.Get("state"))) != 1 {
		s.errorPage(w, r, http.StatusBadRequest, "This login expired or was started in another browser. Start it again from /login.")
		return
	}
	if e := q.Get("error"); e != "" {
		s.errorPage(w, r, http.StatusUnauthorized, fmt.Sprintf("The identity provider refused the login: %.80s", e))
		return
	}
	name, err := s.cfg.OIDC.Login(r.Context(), q.Get("code"), l.Verifier, l.Nonce)
	if err != nil {
		log.Printf("ui: oidc login: %v", err)
		s.errorPage(w, r, http.StatusUnauthorized, "The identity provider's answer did not verify; see the server log. Start again from /login.")
		return
	}
	u, err := s.cfg.Store.UserByName(name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if u == nil {
		log.Printf("ui: oidc login: no hub user for the claim %s", s.cfg.OIDC.UsernameClaim)
		s.errorPage(w, r, http.StatusForbidden, fmt.Sprintf(
			"The identity provider vouches for %q, but this hub has no user of that name. OIDC creates nobody: an admin adds the user with jolitz-hub user add %s --role viewer (or operator, admin).",
			name, name))
		return
	}
	secret, err := s.cfg.Store.StartWebSession(u.Name, WebSessionTTL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	log.Printf("ui: oidc login of %s", u.Name)
	http.SetCookie(w, &http.Cookie{
		Name: api.CookieName, Value: secret, Path: "/", HttpOnly: true, Secure: isTLS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(WebSessionTTL.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
