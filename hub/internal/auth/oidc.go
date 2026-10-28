// Package auth is the hub's OpenID Connect login for the
// browser: the authorization code flow with PKCE (S256), state
// and nonce, discovery through /.well-known/openid-configuration
// and ID tokens verified against the issuer's JWKS with RS256 or
// ES256. Only the standard library: JWKS parsing, key selection
// by kid, signature and claim checks are here.
//
// The provider answers one question, "whom did the issuer vouch
// for?": issuer, subject and the name claim. Mapping that to a
// hub user, the binding of the user to issuer and subject, and
// the session cookie belong to internal/ui. Bearer tokens for jolitz-sync do
// not pass through here.
package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrToken: an ID token that does not verify. The wrapped text
// says which check failed; it never holds the token.
var ErrToken = errors.New("id token rejected")

// Skew is the clock difference tolerated on exp, iat and nbf.
const Skew = time.Minute

// refetchEvery bounds how often an unknown kid makes the
// provider fetch the key set again.
const refetchEvery = time.Minute

// maxResponse bounds every answer read from the issuer.
const maxResponse = 1 << 20

// Provider is one configured issuer. Discovery and keys are
// fetched on first use and cached; a key set is fetched again
// when a token names a kid it lacks, at most once a minute.
type Provider struct {
	Issuer        string
	ClientID      string
	ClientSecret  string // "" for a public client: PKCE only
	RedirectURL   string
	UsernameClaim string
	Client        *http.Client
	Now           func() time.Time

	mu     sync.Mutex
	disc   *discovery
	keys   []jwk
	keysAt time.Time
}

// New returns a provider; nothing is fetched yet.
func New(issuer, clientID, clientSecret, redirectURL, usernameClaim string) *Provider {
	return &Provider{
		Issuer: strings.TrimRight(issuer, "/"), ClientID: clientID, ClientSecret: clientSecret,
		RedirectURL: redirectURL, UsernameClaim: usernameClaim,
		Client: &http.Client{Timeout: 10 * time.Second},
		Now:    time.Now,
	}
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// Login is what one login attempt must remember between the
// redirect to the issuer and the callback: state (against CSRF),
// nonce (binds the ID token to this attempt) and the PKCE
// verifier (binds the code to it).
type Login struct {
	State, Nonce, Verifier string
}

func random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewLogin makes fresh random values for one attempt.
func NewLogin() (Login, error) {
	var l Login
	var err error
	for _, p := range []*string{&l.State, &l.Nonce, &l.Verifier} {
		if *p, err = random(); err != nil {
			return Login{}, err
		}
	}
	return l, nil
}

// challenge is the PKCE S256 code challenge of a verifier.
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (p *Provider) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	res, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", u, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(v)
}

// discover fetches and checks the issuer's metadata once.
func (p *Provider) discover(ctx context.Context) (*discovery, error) {
	p.mu.Lock()
	d := p.disc
	p.mu.Unlock()
	if d != nil {
		return d, nil
	}
	var nd discovery
	if err := p.getJSON(ctx, p.Issuer+"/.well-known/openid-configuration", &nd); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	// The document must be the issuer's own (OpenID Connect
	// Discovery 1.0, 4.3); a trailing slash is the one
	// difference tolerated, since the config drops it.
	if strings.TrimRight(nd.Issuer, "/") != p.Issuer {
		return nil, fmt.Errorf("oidc discovery: issuer %q is not the configured %q", nd.Issuer, p.Issuer)
	}
	if nd.AuthorizationEndpoint == "" || nd.TokenEndpoint == "" || nd.JWKSURI == "" {
		return nil, errors.New("oidc discovery: authorization_endpoint, token_endpoint or jwks_uri missing")
	}
	p.mu.Lock()
	p.disc = &nd
	p.mu.Unlock()
	return &nd, nil
}

// AuthURL is where the browser goes to log in.
func (p *Provider) AuthURL(ctx context.Context, l Login) (string, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {p.RedirectURL},
		"scope":                 {"openid profile email"},
		"state":                 {l.State},
		"nonce":                 {l.Nonce},
		"code_challenge":        {challenge(l.Verifier)},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(d.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return d.AuthorizationEndpoint + sep + q.Encode(), nil
}

// Exchange redeems an authorization code at the token endpoint
// and returns the raw ID token. A confidential client sends its
// secret with HTTP Basic (client_secret_basic, the default of
// RFC 6749 2.3.1, both parts form-encoded).
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (string, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.RedirectURL},
		"code_verifier": {verifier},
		"client_id":     {p.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if p.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(p.ClientID), url.QueryEscape(p.ClientSecret))
	}
	res, err := p.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc token endpoint: %w", err)
	}
	defer res.Body.Close()
	var body struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oidc token endpoint: HTTP %d %s", res.StatusCode, safeWord(body.Error))
	}
	if body.IDToken == "" {
		return "", errors.New("oidc token endpoint: no id_token in the answer")
	}
	return body.IDToken, nil
}

// safeWord keeps an OAuth error code printable and short.
func safeWord(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 64 {
			break
		}
		if r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Identity is what a verified ID token says about the user. Issuer
// and Subject (iss, sub) identify the account at the provider
// for good; Name is the configured username claim, which many
// providers let the user change.
type Identity struct {
	Issuer, Subject, Name string
}

// Login finishes an attempt: code exchange, verification, the
// identity.
func (p *Provider) Login(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	raw, err := p.Exchange(ctx, code, verifier)
	if err != nil {
		return Identity{}, err
	}
	claims, err := p.Verify(ctx, raw, nonce)
	if err != nil {
		return Identity{}, err
	}
	return p.Identify(claims)
}

// Identify reads the identity from verified claims: sub must be a
// non-empty string, the username claim too. With the claim
// "email" the provider must also say email_verified: true, or
// anyone could claim an address they do not own.
func (p *Provider) Identify(claims map[string]any) (Identity, error) {
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return Identity{}, tokenErr("claim sub missing or not a string")
	}
	iss, _ := claims["iss"].(string)
	name, err := p.Username(claims)
	if err != nil {
		return Identity{}, err
	}
	if p.UsernameClaim == "email" {
		if v, _ := claims["email_verified"].(bool); !v {
			return Identity{}, tokenErr("username_claim is email, but email_verified is not true")
		}
	}
	return Identity{Issuer: iss, Subject: sub, Name: name}, nil
}

// Username reads the configured claim; it must be a non-empty
// string.
func (p *Provider) Username(claims map[string]any) (string, error) {
	v, ok := claims[p.UsernameClaim].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%w: claim %s missing or not a string", ErrToken, p.UsernameClaim)
	}
	return v, nil
}

// ---- keys --------------------------------------------------

// jwk is one usable key of the issuer's set.
type jwk struct {
	kid string
	kty string // "RSA" or "EC"
	key crypto.PublicKey
}

type rawJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func b64int(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil || len(b) == 0 {
		return nil, errors.New("bad base64url integer")
	}
	return new(big.Int).SetBytes(b), nil
}

// parseJWK turns an RSA (2048 bits or more) or P-256 key into a
// public key; anything else, or a key for encryption, is nil.
func parseJWK(r rawJWK) *jwk {
	if r.Use != "" && r.Use != "sig" {
		return nil
	}
	switch r.Kty {
	case "RSA":
		if r.Alg != "" && r.Alg != "RS256" {
			return nil
		}
		n, err := b64int(r.N)
		if err != nil || n.BitLen() < 2048 {
			return nil
		}
		e, err := b64int(r.E)
		if err != nil || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 || e.Bit(0) == 0 {
			return nil
		}
		return &jwk{kid: r.Kid, kty: "RSA", key: &rsa.PublicKey{N: n, E: int(e.Int64())}}
	case "EC":
		if r.Crv != "P-256" || r.Alg != "" && r.Alg != "ES256" {
			return nil
		}
		x, err1 := base64.RawURLEncoding.DecodeString(r.X)
		y, err2 := base64.RawURLEncoding.DecodeString(r.Y)
		if err1 != nil || err2 != nil || len(x) != 32 || len(y) != 32 {
			return nil
		}
		// The parser checks that the point is on the curve.
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return nil
		}
		return &jwk{kid: r.Kid, kty: "EC", key: pub}
	}
	return nil
}

func (p *Provider) fetchKeys(ctx context.Context) error {
	d, err := p.discover(ctx)
	if err != nil {
		return err
	}
	var set struct {
		Keys []rawJWK `json:"keys"`
	}
	p.mu.Lock()
	p.keysAt = p.Now()
	p.mu.Unlock()
	if err := p.getJSON(ctx, d.JWKSURI, &set); err != nil {
		return fmt.Errorf("oidc jwks: %w", err)
	}
	var keys []jwk
	for _, r := range set.Keys {
		if k := parseJWK(r); k != nil {
			keys = append(keys, *k)
		}
	}
	p.mu.Lock()
	p.keys = keys
	p.mu.Unlock()
	return nil
}

// key selects the key for a token: by kid when the header has
// one, else the only key of the algorithm's type. A kid the set
// lacks makes one fresh fetch, at most once per refetchEvery.
func (p *Provider) key(ctx context.Context, kid, kty string) (crypto.PublicKey, error) {
	find := func() (crypto.PublicKey, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		var match []jwk
		for _, k := range p.keys {
			if kid != "" && k.kid == kid || kid == "" && k.kty == kty {
				match = append(match, k)
			}
		}
		switch {
		case len(match) == 0:
			return nil, nil
		case len(match) > 1:
			return nil, fmt.Errorf("%w: no kid, and %d keys could have signed", ErrToken, len(match))
		case match[0].kty != kty:
			return nil, fmt.Errorf("%w: key %q is %s, the token says %s", ErrToken, kid, match[0].kty, kty)
		}
		return match[0].key, nil
	}
	p.mu.Lock()
	fetched, at := p.keys != nil, p.keysAt
	p.mu.Unlock()
	if fetched {
		if k, err := find(); k != nil || err != nil {
			return k, err
		}
	}
	if !fetched || p.Now().Sub(at) >= refetchEvery {
		if err := p.fetchKeys(ctx); err != nil {
			return nil, err
		}
		if k, err := find(); k != nil || err != nil {
			return k, err
		}
	}
	return nil, fmt.Errorf("%w: unknown kid %q", ErrToken, kid)
}

// ---- verification ------------------------------------------

func tokenErr(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrToken}, a...)...)
}

// Verify checks a compact ID token: RS256 or ES256 signature by
// a key of the issuer, iss, aud (and azp), exp, iat, nbf and the
// nonce of this login. It returns the claims.
func (p *Provider) Verify(ctx context.Context, raw, nonce string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, tokenErr("not a compact JWS")
	}
	hb, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	pb, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, tokenErr("bad base64url")
	}
	var h struct {
		Alg  string `json:"alg"`
		Kid  string `json:"kid"`
		Crit []any  `json:"crit"`
	}
	if err := json.Unmarshal(hb, &h); err != nil {
		return nil, tokenErr("header: %v", err)
	}
	if h.Crit != nil {
		return nil, tokenErr("crit header not supported")
	}
	var kty string
	switch h.Alg {
	case "RS256":
		kty = "RSA"
	case "ES256":
		kty = "EC"
	default:
		return nil, tokenErr("alg %q (RS256 or ES256 only)", safeWord(h.Alg))
	}
	if _, err := p.discover(ctx); err != nil {
		return nil, err
	}
	pub, err := p.key(ctx, h.Kid, kty)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch k := pub.(type) {
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
			return nil, tokenErr("signature does not verify")
		}
	case *ecdsa.PublicKey:
		if len(sig) != 64 {
			return nil, tokenErr("signature does not verify (ES256 wants 64 bytes)")
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(k, digest[:], r, s) {
			return nil, tokenErr("signature does not verify")
		}
	default:
		return nil, tokenErr("no usable key")
	}

	var claims map[string]any
	dec := json.NewDecoder(bytes.NewReader(pb))
	dec.UseNumber()
	if err := dec.Decode(&claims); err != nil {
		return nil, tokenErr("payload: %v", err)
	}
	p.mu.Lock()
	issuer := p.disc.Issuer
	p.mu.Unlock()
	if iss, _ := claims["iss"].(string); iss != issuer {
		return nil, tokenErr("iss %q is not %q", iss, issuer)
	}
	var auds []string
	switch a := claims["aud"].(type) {
	case string:
		auds = []string{a}
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				auds = append(auds, s)
			}
		}
	}
	found := false
	for _, a := range auds {
		found = found || a == p.ClientID
	}
	if !found {
		return nil, tokenErr("aud does not name client %q", p.ClientID)
	}
	azp, hasAzp := claims["azp"].(string)
	if len(auds) > 1 && !hasAzp {
		return nil, tokenErr("several audiences and no azp")
	}
	if hasAzp && azp != p.ClientID {
		return nil, tokenErr("azp %q is not client %q", azp, p.ClientID)
	}
	now := p.Now()
	num := func(name string) (time.Time, bool, error) {
		v, ok := claims[name]
		if !ok {
			return time.Time{}, false, nil
		}
		n, isNum := v.(json.Number)
		if !isNum {
			return time.Time{}, true, tokenErr("%s is not a number", name)
		}
		f, err := n.Float64()
		if err != nil {
			return time.Time{}, true, tokenErr("%s is not a number", name)
		}
		return time.Unix(int64(f), 0), true, nil
	}
	exp, ok, err := num("exp")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, tokenErr("no exp")
	}
	if !now.Before(exp.Add(Skew)) {
		return nil, tokenErr("expired at %s", exp.UTC().Format(time.RFC3339))
	}
	iat, ok, err := num("iat")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, tokenErr("no iat")
	}
	if iat.After(now.Add(Skew)) {
		return nil, tokenErr("iat lies in the future")
	}
	if nbf, ok, err := num("nbf"); err != nil {
		return nil, err
	} else if ok && nbf.After(now.Add(Skew)) {
		return nil, tokenErr("nbf lies in the future")
	}
	got, _ := claims["nonce"].(string)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(nonce)) != 1 {
		return nil, tokenErr("nonce does not match this login")
	}
	return claims, nil
}
