package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // sha1 is the JWK kid algorithm; non-cryptographic use here
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIDP wires up a minimal RS256 issuer entirely in-process so the
// OIDC middleware can be exercised against a real `coreos/go-oidc`
// verifier without bringing the demo-idp container into a unit test.
type fakeIDP struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	audience string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	sum := sha1.Sum(der) //nolint:gosec // non-cryptographic; thumbprint only
	kid := base64.RawURLEncoding.EncodeToString(sum[:])

	idp := &fakeIDP{
		key:      priv,
		kid:      kid,
		audience: "demo-test-audience",
	}

	mux := http.NewServeMux()
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.server.URL,
			"jwks_uri":                              idp.server.URL + "/jwks.json",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"id_token"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		eBytes := new(big.Int).SetInt64(int64(priv.PublicKey.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []any{
				map[string]any{
					"kty": "RSA",
					"use": "sig",
					"alg": "RS256",
					"kid": kid,
					"n":   base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(eBytes),
				},
			},
		})
	})

	return idp
}

// signToken signs a JWT for the given subject + audience. Caller controls
// audience so negative-test paths can produce a token with the wrong aud.
func (i *fakeIDP) signToken(t *testing.T, sub, aud string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    i.server.URL,
		Subject:   sub,
		Audience:  jwt.ClaimStrings{aud},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = i.kid
	signed, err := tok.SignedString(i.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func TestOIDCAuth_AcceptsValidJWT(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	mw, err := oidcAuth(context.Background(), idp.server.URL, idp.audience)
	if err != nil {
		t.Fatalf("oidcAuth: %v", err)
	}

	store := NewStore()
	store.Seed(SeedRecords())
	mux := http.NewServeMux()
	mux.Handle("GET /api/oauth2flow", mw(listHandler(store)))
	apiSrv := httptest.NewServer(mux)
	t.Cleanup(apiSrv.Close)

	tok := idp.signToken(t, "alice", idp.audience)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, apiSrv.URL+"/api/oauth2flow", http.NoBody)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200; body: %s", resp.StatusCode, string(body))
	}
}

func TestOIDCAuth_RejectsMissingHeader(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	mw, err := oidcAuth(context.Background(), idp.server.URL, idp.audience)
	if err != nil {
		t.Fatalf("oidcAuth: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/oauth2flow", mw(listHandler(NewStore())))
	apiSrv := httptest.NewServer(mux)
	t.Cleanup(apiSrv.Close)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, apiSrv.URL+"/api/oauth2flow", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", resp.Header.Get("WWW-Authenticate"))
	}
}

func TestOIDCAuth_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	idp := newFakeIDP(t)
	mw, err := oidcAuth(context.Background(), idp.server.URL, idp.audience)
	if err != nil {
		t.Fatalf("oidcAuth: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/oauth2flow", mw(listHandler(NewStore())))
	apiSrv := httptest.NewServer(mux)
	t.Cleanup(apiSrv.Close)

	tok := idp.signToken(t, "alice", "different-audience")
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, apiSrv.URL+"/api/oauth2flow", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestOIDCAuth_FactoryRejectsEmptyArgs(t *testing.T) {
	t.Parallel()

	if _, err := oidcAuth(context.Background(), "", "aud"); err == nil {
		t.Error("issuer empty: want error")
	}
	if _, err := oidcAuth(context.Background(), "http://x", ""); err == nil {
		t.Error("audience empty: want error")
	}
}

func TestBuildOIDCMiddleware_ReturnsNilWhenEnvUnset(t *testing.T) {
	// Cannot t.Parallel() — t.Setenv mutates process env.
	t.Setenv("DEMO_OIDC_ISSUER", "")
	t.Setenv("DEMO_OIDC_AUDIENCE", "")
	mw, err := buildOIDCMiddleware(t.Context())
	if err != nil {
		t.Fatalf("buildOIDCMiddleware: %v", err)
	}
	if mw != nil {
		t.Error("mw = non-nil, want nil sentinel when env unset")
	}
}

func TestUnavailableMiddleware_Returns503(t *testing.T) {
	t.Parallel()

	store := NewStore()
	store.Seed(SeedRecords())
	mux := http.NewServeMux()
	mux.Handle("GET /api/oauth2flow", oidcUnavailableMiddleware(listHandler(store)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/oauth2flow", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
