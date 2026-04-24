package ir

import "time"

// AuthSpec is the sealed sum type representing server-facing authentication.
// Exactly one concrete variant is instantiated per Spec.Auth. The
// unexported isAuthSpec method prevents external packages from adding new
// variants — all downstream code must handle the closed set.
type AuthSpec interface {
	isAuthSpec()
}

// AuthNone represents an explicitly unauthenticated MCP server. The
// generator emits a stderr warning at generate time and a `// WARNING: no
// authentication configured` comment in the generated file.
type AuthNone struct{}

// AuthBearer is the static-map bearer scheme: tokens are loaded from an
// env var at startup and matched against incoming Authorization headers.
type AuthBearer struct {
	TokensEnv    string
	SubjectClaim string
}

// AuthOIDC is the fixed-issuer / fixed-JWKS OIDC scheme. No discovery call
// is made at startup.
type AuthOIDC struct {
	Issuer         string
	JWKSURL        string
	Audience       string
	RequiredScopes []string
	SubjectClaim   string
}

// AuthOIDCDynamic is the dynamic-discovery scheme. The generated server
// fetches .well-known/openid-configuration at startup and caches JWKS per
// CacheTTL.
type AuthOIDCDynamic struct {
	Issuer         string
	Audience       string
	RequiredScopes []string
	SubjectClaim   string
	CacheTTL       time.Duration
}

func (AuthNone) isAuthSpec()        {}
func (AuthBearer) isAuthSpec()      {}
func (AuthOIDC) isAuthSpec()        {}
func (AuthOIDCDynamic) isAuthSpec() {}
