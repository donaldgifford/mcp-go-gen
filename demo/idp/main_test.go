package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// newTestServer wires a server with a fresh RSA key against a synthetic
// issuer URL. Tests use this rather than `run` to skip the listener +
// signal handling, which aren't relevant to handler behavior.
func newTestServer(t *testing.T) *server {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	kid, err := computeKid(&priv.PublicKey)
	if err != nil {
		t.Fatalf("computeKid: %v", err)
	}
	return &server{
		issuer:   "http://test-idp",
		audience: "test-audience",
		key:      priv,
		kid:      kid,
	}
}

func TestDiscovery_HasRequiredFields(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.handleDiscovery(rr, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"issuer", "jwks_uri", "id_token_signing_alg_values_supported"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("discovery doc missing %q", key)
		}
	}
	if doc["issuer"] != srv.issuer {
		t.Errorf("issuer = %v, want %s", doc["issuer"], srv.issuer)
	}
}

func TestJWKS_ContainsOneKey(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.handleJWKS(rr, httptest.NewRequest(http.MethodGet, "/jwks.json", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(resp.Keys))
	}
	jwk := resp.Keys[0]
	for _, key := range []string{"kty", "use", "alg", "kid", "n", "e"} {
		if _, ok := jwk[key]; !ok {
			t.Errorf("jwk missing %q field", key)
		}
	}
	if jwk["kid"] != srv.kid {
		t.Errorf("kid = %v, want %s", jwk["kid"], srv.kid)
	}
}

func TestToken_DefaultSubjectAndAudience(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.handleToken(rr, httptest.NewRequest(http.MethodGet, "/token", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token = empty string")
	}
	parsed, err := jwt.ParseWithClaims(resp.AccessToken, &jwt.RegisteredClaims{}, func(_ *jwt.Token) (any, error) {
		return &srv.key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse signed token: %v", err)
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok {
		t.Fatalf("claims type = %T", parsed.Claims)
	}
	if claims.Subject != "alice" {
		t.Errorf("Subject = %q, want alice (default)", claims.Subject)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != srv.audience {
		t.Errorf("Audience = %v, want [%s]", claims.Audience, srv.audience)
	}
	if claims.Issuer != srv.issuer {
		t.Errorf("Issuer = %q, want %s", claims.Issuer, srv.issuer)
	}
}

func TestToken_AcceptsCustomSubAndAud(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.handleToken(rr, httptest.NewRequest(http.MethodGet, "/token?sub=bob&aud=other-aud", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	parsed, err := jwt.ParseWithClaims(resp.AccessToken, &jwt.RegisteredClaims{}, func(_ *jwt.Token) (any, error) {
		return &srv.key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse signed token: %v", err)
	}
	claims := parsed.Claims.(*jwt.RegisteredClaims)
	if claims.Subject != "bob" {
		t.Errorf("Subject = %q, want bob", claims.Subject)
	}
	if !strings.Contains(strings.Join(claims.Audience, ","), "other-aud") {
		t.Errorf("Audience = %v, want [other-aud]", claims.Audience)
	}
}
