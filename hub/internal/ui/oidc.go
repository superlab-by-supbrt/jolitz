package ui

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/api"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/auth"
	"github.com/superlab-by-supbrt/jolitz/hub/internal/store"
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
// user, the user's binding to issuer and subject checked (or
// made, on the first login). Then the same login cookie as the token login, holding
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
	id, err := s.cfg.OIDC.Login(r.Context(), q.Get("code"), l.Verifier, l.Nonce)
	if err != nil {
		log.Printf("ui: oidc login: %v", err)
		s.errorPage(w, r, http.StatusUnauthorized, "The identity provider's answer did not verify; see the server log. Start again from /login.")
		return
	}
	u, err := s.cfg.Store.UserByName(id.Name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if u == nil {
		log.Printf("ui: oidc login: no hub user for the claim %s", s.cfg.OIDC.UsernameClaim)
		s.errorPage(w, r, http.StatusForbidden, fmt.Sprintf(
			"The identity provider vouches for %q, but this hub has no user of that name. OIDC creates nobody: an admin adds the user with jolitz-hub user add %s --role viewer (or operator, admin).",
			id.Name, id.Name))
		return
	}
	if !s.checkBinding(w, r, u.Name, id) {
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

// subjectTag stands for a subject in the log: the first twelve
// hex digits of its SHA-256, never the subject itself.
func subjectTag(sub string) string {
	sum := sha256.Sum256([]byte(sub))
	return hex.EncodeToString(sum[:])[:12]
}

// checkBinding holds a login to the identity the user is bound
// to. The username claim only finds the user; many providers let
// a user change it. A bound user must come with the same issuer
// and subject; an unbound one is bound to this pair now (the
// first login wins), unless the pair belongs to another user.
// It writes the 403 page itself and reports whether the login
// may go on.
func (s *server) checkBinding(w http.ResponseWriter, r *http.Request, user string, id auth.Identity) bool {
	reset := fmt.Sprintf("An admin resets the binding with jolitz-hub user unbind %s; the next OIDC login binds it again.", user)
	boundElsewhere := func() {
		s.errorPage(w, r, http.StatusForbidden, fmt.Sprintf(
			"The hub user %s is bound to another identity at the identity provider; this login is not that account. %s", user, reset))
	}
	iss, sub, ok, err := s.cfg.Store.OIDCBinding(user)
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	if ok {
		if iss == id.Issuer && sub == id.Subject {
			return true
		}
		log.Printf("ui: oidc login: %s is bound to %s subject %s, the token is %s subject %s",
			user, iss, subjectTag(sub), id.Issuer, subjectTag(id.Subject))
		boundElsewhere()
		return false
	}
	other, err := s.cfg.Store.UserByOIDC(id.Issuer, id.Subject)
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	if other == "" {
		err = s.cfg.Store.BindOIDC(user, id.Issuer, id.Subject)
		if err == nil {
			log.Printf("ui: oidc: bound %s to %s subject %s", user, id.Issuer, subjectTag(id.Subject))
			return true
		}
		if !errors.Is(err, store.ErrConflict) {
			s.fail(w, r, err)
			return false
		}
		// Another login bound one of the two meanwhile.
		other, _ = s.cfg.Store.UserByOIDC(id.Issuer, id.Subject)
		if other == "" || other == user {
			boundElsewhere()
			return false
		}
	}
	log.Printf("ui: oidc login: %s subject %s is bound to %s, not %s", id.Issuer, subjectTag(id.Subject), other, user)
	s.errorPage(w, r, http.StatusForbidden, fmt.Sprintf(
		"This identity at the identity provider is already bound to another hub user (named in the server log), not to %s. An admin resets that binding with jolitz-hub user unbind followed by that user's name.", user))
	return false
}
