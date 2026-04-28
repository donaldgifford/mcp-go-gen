// Package main runs the demo OIDC issuer for IMPL-0002 Phase 5 — a
// minimal RS256 JWT signer that exposes only the two endpoints the
// `coreos/go-oidc` verifier requires (discovery + JWKS) plus a
// convenience `/token` endpoint that mints a JWT for a given subject so
// inspector users can `curl` for a token and paste it into the UI.
//
// Per INV-0001: this is intentionally not a production-grade issuer.
// No DB, no client registration, no PKCE, no key rotation. The signing
// key is generated at startup and held in memory; restarting the
// container invalidates every previously issued token.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // sha1 is the JWK kid algorithm; non-cryptographic use here
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAddr     = ":5556"
	defaultIssuer   = "http://demo-idp:5556"
	defaultAudience = "mcp-demo-api"
	tokenLifetime   = time.Hour
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := envDefault("IDP_ADDR", defaultAddr)
	issuer := envDefault("IDP_ISSUER", defaultIssuer)
	audience := envDefault("IDP_AUDIENCE", defaultAudience)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}
	kid, err := computeKid(&priv.PublicKey)
	if err != nil {
		return fmt.Errorf("compute kid: %w", err)
	}

	srv := &server{
		issuer:   issuer,
		audience: audience,
		key:      priv,
		kid:      kid,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", srv.handleDiscovery)
	mux.HandleFunc("GET /jwks.json", srv.handleJWKS)
	mux.HandleFunc("GET /token", srv.handleToken)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           accessLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("demo idp listening", "addr", httpServer.Addr, "issuer", issuer, "audience", audience, "kid", kid)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type server struct {
	issuer   string
	audience string
	key      *rsa.PrivateKey
	kid      string
}

// handleDiscovery returns the minimal OIDC discovery document `coreos/go-oidc`
// reads on `oidc.NewProvider(ctx, issuer)`. Only `issuer` and `jwks_uri` are
// load-bearing for verification; the others are present so the discovery
// document parses against the OIDC spec.
func (s *server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                s.issuer,
		"jwks_uri":                              s.issuer + "/jwks.json",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleJWKS returns the public half of the signing key in JWK form. `kid`
// matches the value embedded in every issued JWT's header so the verifier
// can pick the right key.
func (s *server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := s.key.PublicKey
	jwk := map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": s.kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(intToBytes(pub.E)),
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": []any{jwk}})
}

// handleToken mints a JWT for the requested subject. Defaults to "alice"
// when ?sub is absent so a bare `curl …/token` returns a usable token.
//
// The audience can be overridden via ?aud — handy for negative tests where
// the inspector should see a 401 from the demo API because the proxy bearer
// is signed for the wrong audience.
func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimSpace(r.URL.Query().Get("sub"))
	if sub == "" {
		sub = "alice"
	}
	aud := strings.TrimSpace(r.URL.Query().Get("aud"))
	if aud == "" {
		aud = s.audience
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    s.issuer,
		Subject:   sub,
		Audience:  jwt.ClaimStrings{aud},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenLifetime)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	signed, err := token.SignedString(s.key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": signed,
		"token_type":   "Bearer",
		"expires_in":   int(tokenLifetime.Seconds()),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json", "err", err)
	}
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// intToBytes encodes a positive int as a big-endian byte slice with leading
// zeros stripped — JWK's "e" parameter wants the raw exponent bytes, not a
// fixed-width int. Routed through math/big so the conversion is platform
// independent.
func intToBytes(i int) []byte {
	return new(big.Int).SetInt64(int64(i)).Bytes()
}

// computeKid derives a stable key id from the public key's DER encoding so
// JWT headers and JWKS entries reference the same kid across restarts of
// the same container instance.
func computeKid(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal pubkey: %w", err)
	}
	sum := sha1.Sum(der) //nolint:gosec // non-cryptographic; thumbprint only
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// accessLog wraps next with one slog line per request.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request", //nolint:gosec // G706: JSON-escaped via slog handler
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
