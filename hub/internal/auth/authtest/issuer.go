// Package authtest is an OpenID Connect issuer for tests: an
// httptest server with discovery, a JWKS holding one RSA and one
// P-256 key, an authorization step the test drives by hand and a
// token endpoint that checks the client, the redirect URI and
// the PKCE verifier before it signs an ID token.
package authtest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Issuer is the fake identity provider.
type Issuer struct {
	*httptest.Server
	ClientID     string
	ClientSecret string
	RSA          *rsa.PrivateKey
	EC           *ecdsa.PrivateKey
	// Alg and KID choose the key that signs ID tokens: RS256 with
	// kid "rsa-1" or ES256 with kid "ec-1" by default.
	Alg, KID string
	// Mutate, when set, edits the claims of every ID token the
	// token endpoint signs (wrong aud, past exp, …).
	Mutate func(map[string]any)
	Now    func() time.Time

	mu    sync.Mutex
	codes map[string]grant
	// JWKSFetches counts the GETs of the key set.
	JWKSFetches int
}

type grant struct {
	challenge, nonce, redirect string
	claims                     map[string]any
}

// New starts an issuer; it stops with the test.
func New(t *testing.T) *Issuer {
	t.Helper()
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	i := &Issuer{ClientID: "jolitz-hub", ClientSecret: "test-secret", RSA: rk, EC: ek,
		Alg: "RS256", KID: "rsa-1", Now: time.Now, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET /jwks", i.jwks)
	mux.HandleFunc("POST /token", i.token)
	i.Server = httptest.NewServer(mux)
	t.Cleanup(i.Close)
	return i
}

func (i *Issuer) discovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"issuer":                                i.URL,
		"authorization_endpoint":                i.URL + "/authorize",
		"token_endpoint":                        i.URL + "/token",
		"jwks_uri":                              i.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"id_token_signing_alg_values_supported": []string{"RS256", "ES256"},
	})
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func pad32(n *big.Int) []byte {
	b := make([]byte, 32)
	n.FillBytes(b)
	return b
}

func (i *Issuer) jwks(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	i.JWKSFetches++
	i.mu.Unlock()
	writeJSON(w, 200, map[string]any{"keys": []map[string]any{
		{"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256",
			"n": b64(i.RSA.N.Bytes()), "e": b64(big.NewInt(int64(i.RSA.E)).Bytes())},
		{"kty": "EC", "kid": "ec-1", "use": "sig", "alg": "ES256", "crv": "P-256",
			"x": b64(pad32(i.EC.X)), "y": b64(pad32(i.EC.Y))},
	}})
}

// Authorize plays the user at the issuer's login page: it takes
// the authorization URL the hub redirected to, checks its
// parameters and returns the code and state the issuer would
// send back to the redirect URI. claims are the user's (e.g.
// preferred_username).
func (i *Issuer) Authorize(t *testing.T, authURL string, claims map[string]any) (code, state string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, i.URL+"/authorize?") {
		t.Fatalf("authorization URL %q", authURL)
	}
	q := u.Query()
	for k, want := range map[string]string{"response_type": "code", "client_id": i.ClientID, "code_challenge_method": "S256"} {
		if q.Get(k) != want {
			t.Fatalf("authorization URL: %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if !strings.Contains(" "+q.Get("scope")+" ", " openid ") {
		t.Fatalf("scope %q lacks openid", q.Get("scope"))
	}
	for _, k := range []string{"state", "nonce", "code_challenge", "redirect_uri"} {
		if q.Get(k) == "" {
			t.Fatalf("authorization URL lacks %s", k)
		}
	}
	raw := make([]byte, 16)
	rand.Read(raw)
	code = b64(raw)
	i.mu.Lock()
	i.codes[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), claims: claims}
	i.mu.Unlock()
	return code, q.Get("state")
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	if !ok || id != i.ClientID || secret != i.ClientSecret {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	i.mu.Lock()
	g, found := i.codes[r.PostForm.Get("code")]
	delete(i.codes, r.PostForm.Get("code"))
	i.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case r.PostForm.Get("grant_type") != "authorization_code", !found:
		writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
		return
	case b64(sum[:]) != g.challenge:
		writeJSON(w, 400, map[string]string{"error": "invalid_grant", "error_description": "PKCE verifier mismatch"})
		return
	case r.PostForm.Get("redirect_uri") != g.redirect:
		writeJSON(w, 400, map[string]string{"error": "invalid_grant", "error_description": "redirect_uri mismatch"})
		return
	}
	now := i.Now()
	claims := map[string]any{"iss": i.URL, "aud": i.ClientID, "sub": "user-1",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce}
	for k, v := range g.claims {
		claims[k] = v
	}
	if i.Mutate != nil {
		i.Mutate(claims)
	}
	writeJSON(w, 200, map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300,
		"id_token": i.Sign(i.Alg, i.KID, claims)})
}

// Sign makes a compact JWS of claims with the issuer's RSA key
// (RS256) or P-256 key (ES256); kid "" leaves the header
// without one.
func (i *Issuer) Sign(alg, kid string, claims map[string]any) string {
	h := map[string]any{"alg": alg, "typ": "JWT"}
	if kid != "" {
		h["kid"] = kid
	}
	hb, _ := json.Marshal(h)
	cb, _ := json.Marshal(claims)
	input := b64(hb) + "." + b64(cb)
	sum := sha256.Sum256([]byte(input))
	var sig []byte
	switch alg {
	case "RS256":
		sig, _ = rsa.SignPKCS1v15(rand.Reader, i.RSA, crypto.SHA256, sum[:])
	case "ES256":
		r, s, _ := ecdsa.Sign(rand.Reader, i.EC, sum[:])
		sig = append(pad32(r), pad32(s)...)
	default:
		panic(fmt.Sprintf("authtest: alg %s", alg))
	}
	return input + "." + b64(sig)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
