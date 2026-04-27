package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerAuth wraps next with an Authorization: Bearer check. Returns 401
// on missing or wrong token; uses constant-time compare to avoid timing
// signal on the secret.
//
// The expected token is captured by closure at startup — env-driven and
// constant for the process lifetime.
func bearerAuth(expected string) func(http.Handler) http.Handler {
	expectedBytes := []byte(expected)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := bearerFromRequest(r)
			if !ok || subtle.ConstantTimeCompare([]byte(got), expectedBytes) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerFromRequest extracts the token after "Bearer " from the
// Authorization header. Returns "", false on missing or malformed.
func bearerFromRequest(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}
