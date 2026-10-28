package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superlab-by-supbrt/jolitz/hub/internal/auth/authtest"
)

func newProvider(iss *authtest.Issuer) *Provider {
	return New(iss.URL, iss.ClientID, iss.ClientSecret, "https://hub.example.com/oidc/callback", "preferred_username")
}

func goodClaims(iss *authtest.Issuer, now time.Time) map[string]any {
	return map[string]any{"iss": iss.URL, "aud": iss.ClientID, "sub": "u1",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": "n-1",
		"preferred_username": "alice"}
}

// The whole flow: discovery, authorization URL with PKCE and
// state, code exchange, signature and claim checks.
func TestLoginFlow(t *testing.T) {
	for _, alg := range []struct{ alg, kid string }{{"RS256", "rsa-1"}, {"ES256", "ec-1"}} {
		t.Run(alg.alg, func(t *testing.T) {
			iss := authtest.New(t)
			iss.Alg, iss.KID = alg.alg, alg.kid
			p := newProvider(iss)
			l, err := NewLogin()
			if err != nil {
				t.Fatal(err)
			}
			au, err := p.AuthURL(context.Background(), l)
			if err != nil {
				t.Fatal(err)
			}
			q, _ := url.Parse(au)
			if q.Query().Get("state") != l.State || q.Query().Get("nonce") != l.Nonce ||
				q.Query().Get("redirect_uri") != "https://hub.example.com/oidc/callback" {
				t.Errorf("auth URL %s", au)
			}
			if q.Query().Get("code_challenge") == "" || strings.Contains(au, l.Verifier) {
				t.Error("the verifier leaks into the authorization URL")
			}
			code, _ := iss.Authorize(t, au, map[string]any{"preferred_username": "alice"})
			name, err := p.Login(context.Background(), code, l.Verifier, l.Nonce)
			if err != nil || name != "alice" {
				t.Fatalf("login = %q, %v", name, err)
			}
		})
	}
}

func TestExchangeNeedsTheVerifier(t *testing.T) {
	iss := authtest.New(t)
	p := newProvider(iss)
	l, _ := NewLogin()
	au, _ := p.AuthURL(context.Background(), l)
	code, _ := iss.Authorize(t, au, map[string]any{"preferred_username": "alice"})
	other, _ := NewLogin()
	if _, err := p.Login(context.Background(), code, other.Verifier, l.Nonce); err == nil {
		t.Error("a code was redeemed with the wrong PKCE verifier")
	}
}

func TestExchangeWrongSecret(t *testing.T) {
	iss := authtest.New(t)
	p := New(iss.URL, iss.ClientID, "wrong", "https://hub.example.com/oidc/callback", "preferred_username")
	l, _ := NewLogin()
	au, _ := p.AuthURL(context.Background(), l)
	code, _ := iss.Authorize(t, au, nil)
	_, err := p.Login(context.Background(), code, l.Verifier, l.Nonce)
	if err == nil || strings.Contains(err.Error(), "wrong") {
		t.Errorf("wrong secret: %v (must fail, must not show the secret)", err)
	}
}

func TestVerify(t *testing.T) {
	iss := authtest.New(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := newProvider(iss)
	p.Now = func() time.Time { return now }
	ctx := context.Background()
	sign := func(alg, kid string, edit func(map[string]any)) string {
		c := goodClaims(iss, now)
		if edit != nil {
			edit(c)
		}
		return iss.Sign(alg, kid, c)
	}
	if c, err := p.Verify(ctx, sign("RS256", "rsa-1", nil), "n-1"); err != nil || c["preferred_username"] != "alice" {
		t.Fatalf("RS256: %v %v", c, err)
	}
	if _, err := p.Verify(ctx, sign("ES256", "ec-1", nil), "n-1"); err != nil {
		t.Fatalf("ES256: %v", err)
	}
	// Without a kid the one key of the algorithm's type is used.
	if _, err := p.Verify(ctx, sign("ES256", "", nil), "n-1"); err != nil {
		t.Errorf("ES256 without kid: %v", err)
	}
	if _, err := p.Verify(ctx, sign("RS256", "rsa-1", func(c map[string]any) { c["aud"] = []any{"jolitz-hub", "other"}; c["azp"] = "jolitz-hub" }), "n-1"); err != nil {
		t.Errorf("aud list with azp: %v", err)
	}

	rsaToken := sign("RS256", "rsa-1", nil)
	parts := strings.Split(rsaToken, ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[0] ^= 1
	tampered := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(sig)
	// The RSA token's header with alg ES256 and kid rsa-1: a key
	// is never used for another algorithm.
	confused := strings.Replace(rsaToken, parts[0], base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","kid":"rsa-1"}`)), 1)
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + parts[1] + "."
	hs := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"rsa-1"}`)) + "." + parts[1] + "." + parts[2]

	for name, c := range map[string]struct {
		token, nonce, want string
	}{
		"wrong aud":          {sign("RS256", "rsa-1", func(c map[string]any) { c["aud"] = "someone-else" }), "n-1", "aud"},
		"aud list sans azp":  {sign("RS256", "rsa-1", func(c map[string]any) { c["aud"] = []any{"jolitz-hub", "other"} }), "n-1", "azp"},
		"foreign azp":        {sign("RS256", "rsa-1", func(c map[string]any) { c["azp"] = "other" }), "n-1", "azp"},
		"expired":            {sign("RS256", "rsa-1", func(c map[string]any) { c["exp"] = now.Add(-2 * time.Minute).Unix() }), "n-1", "expired"},
		"no exp":             {sign("RS256", "rsa-1", func(c map[string]any) { delete(c, "exp") }), "n-1", "exp"},
		"issued in future":   {sign("RS256", "rsa-1", func(c map[string]any) { c["iat"] = now.Add(10 * time.Minute).Unix() }), "n-1", "iat"},
		"not yet valid":      {sign("RS256", "rsa-1", func(c map[string]any) { c["nbf"] = now.Add(10 * time.Minute).Unix() }), "n-1", "nbf"},
		"wrong iss":          {sign("RS256", "rsa-1", func(c map[string]any) { c["iss"] = "https://evil.example" }), "n-1", "iss"},
		"wrong nonce":        {sign("RS256", "rsa-1", nil), "n-2", "nonce"},
		"no nonce":           {sign("RS256", "rsa-1", func(c map[string]any) { delete(c, "nonce") }), "n-1", "nonce"},
		"unknown kid":        {sign("RS256", "rsa-9", nil), "n-1", "kid"},
		"tampered signature": {tampered, "n-1", "signature"},
		"alg confusion":      {confused, "n-1", ""},
		"alg none":           {none, "n-1", "alg"},
		"alg HS256":          {hs, "n-1", "alg"},
		"garbage":            {"a.b", "n-1", ""},
	} {
		_, err := p.Verify(ctx, c.token, c.nonce)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !errors.Is(err, ErrToken) {
			t.Errorf("%s: %v is not ErrToken", name, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want it to name %q", name, err, c.want)
		}
	}
}

// An unknown kid makes the provider fetch the key set again
// once (keys rotate), not on every request.
func TestUnknownKidRefetchesOnce(t *testing.T) {
	iss := authtest.New(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := newProvider(iss)
	p.Now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := p.Verify(ctx, iss.Sign("RS256", "rsa-1", goodClaims(iss, now)), "n-1"); err != nil {
		t.Fatal(err)
	}
	if iss.JWKSFetches != 1 {
		t.Fatalf("fetches %d", iss.JWKSFetches)
	}
	// Within the minute after a fetch, no new one.
	p.Verify(ctx, iss.Sign("RS256", "rotated", goodClaims(iss, now)), "n-1")
	if iss.JWKSFetches != 1 {
		t.Fatalf("fetches right after the first: %d", iss.JWKSFetches)
	}
	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		p.Verify(ctx, iss.Sign("RS256", "rotated", goodClaims(iss, now)), "n-1")
	}
	if iss.JWKSFetches != 2 {
		t.Errorf("fetches after unknown kids: %d, want 2", iss.JWKSFetches)
	}
	now = now.Add(2 * time.Minute)
	p.Verify(ctx, iss.Sign("RS256", "rotated", goodClaims(iss, now)), "n-1")
	if iss.JWKSFetches != 3 {
		t.Errorf("fetches a minute later: %d, want 3", iss.JWKSFetches)
	}
}

func TestUsernameClaim(t *testing.T) {
	iss := authtest.New(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := New(iss.URL, iss.ClientID, iss.ClientSecret, "https://hub.example.com/oidc/callback", "email")
	p.Now = func() time.Time { return now }
	c := goodClaims(iss, now)
	c["email"] = "alice"
	claims, err := p.Verify(context.Background(), iss.Sign("RS256", "rsa-1", c), "n-1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.Username(claims); n != "alice" || err != nil {
		t.Errorf("email claim: %q %v", n, err)
	}
	delete(claims, "email")
	if _, err := p.Username(claims); err == nil {
		t.Error("missing claim accepted")
	}
	claims["email"] = 42.0
	if _, err := p.Username(claims); err == nil {
		t.Error("non-string claim accepted")
	}
}

func TestDiscoveryIssuerMismatch(t *testing.T) {
	iss := authtest.New(t)
	p := New(iss.URL+"/realms/other", iss.ClientID, iss.ClientSecret, "https://hub.example.com/oidc/callback", "preferred_username")
	l, _ := NewLogin()
	if _, err := p.AuthURL(context.Background(), l); err == nil {
		t.Error("discovery from a wrong issuer path accepted")
	}
}
