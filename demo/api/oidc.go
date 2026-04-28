// Package-internal OIDC middleware for the demo API's /api/oauth2flow
// tree. Validates RS256 JWTs against the demo-idp issuer's JWKS using
// `coreos/go-oidc`. Only the issuer + audience are configurable; the
// signing key rotates whenever demo-idp restarts (acceptable for a demo
// per INV-0001).

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
)

// oidcAuth returns an HTTP middleware that requires every incoming request
// to carry a Bearer JWT signed by the configured issuer with a matching
// audience. Verification fetches JWKS from the issuer's discovery
// document at construction time and caches it inside go-oidc.
//
// On success the verified subject lands in the request context under
// oidcSubjectKey so handlers (or future logging) can attribute calls.
func oidcAuth(ctx context.Context, issuer, audience string) (func(http.Handler) http.Handler, error) {
	if issuer == "" {
		return nil, errors.New("oidc issuer is required")
	}
	if audience == "" {
		return nil, errors.New("oidc audience is required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: audience})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerFromRequest(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="oauth2flow"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			token, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="oauth2flow", error="invalid_token", error_description=%q`, err.Error()))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), oidcSubjectKey{}, token.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}

// oidcSubjectKey is the unexported context key for the verified subject.
type oidcSubjectKey struct{}
