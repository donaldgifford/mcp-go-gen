package config

// This file defines the HCL2 schema for mcpgen.hcl, mirroring DESIGN-0004
// §"HCL Schema — Top Level". Everything here is plain Go struct tags
// consumed by gohcl. The structs are intentionally close to the HCL shape —
// cross-field validation and normalization happen during the ToIR step
// rather than in the decoder.

// Config is the root of a decoded mcpgen.hcl. Every nested block is an HCL
// block; every scalar is an HCL attribute.
type Config struct {
	Version string      `hcl:"mcpgen_version"`
	Server  Server      `hcl:"server,block"`
	Proxy   *Proxy      `hcl:"proxy,block"`
	Embed   *Embed      `hcl:"embed,block"`
	Tools   []ToolBlock `hcl:"tool,block"`
}

// Server describes the MCP listener identity, observability, and
// client-facing authentication.
type Server struct {
	Name          string         `hcl:"name"`
	Version       string         `hcl:"version,optional"`
	Listener      Listener       `hcl:"listener,block"`
	Observability *Observability `hcl:"observability,block"`
	Auth          Auth           `hcl:"auth,block"`
}

// Listener is the network address the generated MCP HTTP server binds to.
type Listener struct {
	Addr         string `hcl:"addr"`
	EndpointPath string `hcl:"endpoint_path,optional"`
}

// Observability groups the three defaults-on signal subsystems. All three
// sub-blocks are optional; omitted blocks fall back to generator defaults
// per DESIGN-0004.
type Observability struct {
	Logging *Logging `hcl:"logging,block"`
	Metrics *Metrics `hcl:"metrics,block"`
	Tracing *Tracing `hcl:"tracing,block"`
}

// Logging configures the generated slog handler.
type Logging struct {
	Format string `hcl:"format,optional"` // "json" (default) | "text"
	Level  string `hcl:"level,optional"`  // "debug" | "info" (default) | "warn" | "error"
}

// Metrics configures the generated Prometheus registry and /metrics endpoint.
// When Addr is empty, /metrics is mounted on the same mux as the MCP endpoint;
// otherwise a separate http.Server listens on Addr.
type Metrics struct {
	Enabled *bool  `hcl:"enabled,optional"` // pointer so "unset" is distinguishable from false
	Path    string `hcl:"path,optional"`    // default "/metrics"
	Addr    string `hcl:"addr,optional"`    // "" → shared listener; non-empty → separate server
}

// Tracing configures the OpenTelemetry tracer provider. When Enabled is
// explicitly false, the generator wires a no-op provider instead of OTLP.
type Tracing struct {
	Enabled     *bool   `hcl:"enabled,optional"`
	ServiceName string  `hcl:"service_name,optional"`
	SampleRatio float64 `hcl:"sample_ratio,optional"`
	Exporter    string  `hcl:"exporter,optional"` // "otlp_http" (default)
	Endpoint    string  `hcl:"endpoint,optional"`
}

// Auth is the server-facing auth wrapper. Exactly one of its four sub-blocks
// must be present; enforcement happens in ToIR so that the decoder itself
// emits the clean gohcl diagnostic for the non-auth-specific structural
// issues.
type Auth struct {
	None        *AuthNone        `hcl:"none,block"`
	Bearer      *AuthBearer      `hcl:"bearer,block"`
	OIDC        *AuthOIDC        `hcl:"oidc,block"`
	OIDCDynamic *AuthOIDCDynamic `hcl:"oidc_dynamic,block"`
}

// AuthNone carries no configuration — the block's presence is the signal.
type AuthNone struct{}

// AuthBearer is the server-side static bearer scheme. TokensEnv names the
// env var the generated code reads to populate the token-to-subject map.
type AuthBearer struct {
	TokensEnv    string `hcl:"tokens_env"`
	SubjectClaim string `hcl:"subject_claim,optional"`
}

// AuthOIDC is the fixed-issuer OIDC/JWT scheme. JWKSURL is read up-front
// at startup; no discovery call is made.
type AuthOIDC struct {
	Issuer         string   `hcl:"issuer"`
	JWKSURL        string   `hcl:"jwks_url"`
	Audience       string   `hcl:"audience"`
	RequiredScopes []string `hcl:"required_scopes,optional"`
	SubjectClaim   string   `hcl:"subject_claim,optional"`
}

// AuthOIDCDynamic is the dynamic-discovery OIDC scheme — the generated
// server calls oidc.NewProvider(issuer) at startup to fetch JWKS metadata.
type AuthOIDCDynamic struct {
	Issuer         string   `hcl:"issuer"`
	Audience       string   `hcl:"audience"`
	RequiredScopes []string `hcl:"required_scopes,optional"`
	SubjectClaim   string   `hcl:"subject_claim,optional"`
	CacheTTL       string   `hcl:"cache_ttl,optional"` // duration string, default "1h"
}

// Proxy is the top-level proxy-mode backend configuration. Present only when
// the spec defines at least one proxy-flavored tool.
type Proxy struct {
	BaseURL  string         `hcl:"base_url"`
	Auth     *ProxyAuth     `hcl:"auth,block"`
	OpenAPI  *ProxyOpenAPI  `hcl:"openapi,block"`
	Timeouts *ProxyTimeouts `hcl:"timeouts,block"`
	Retry    *ProxyRetry    `hcl:"retry,block"`
}

// ProxyAuth configures how the generated backend client authenticates to
// the upstream service. It is distinct from server.auth, which governs how
// callers authenticate to the MCP server itself.
type ProxyAuth struct {
	Bearer *ProxyAuthBearer `hcl:"bearer,block"`
}

// ProxyAuthBearer reads a single bearer token from an env var and attaches
// it to every outbound request.
type ProxyAuthBearer struct {
	TokenEnv string `hcl:"token_env"`
}

// ProxyOpenAPI points at an OpenAPI 3.x document used to resolve
// openapi_operation references on tool blocks.
type ProxyOpenAPI struct {
	Spec string `hcl:"spec"`
}

// ProxyTimeouts maps to the generated http.Transport and Client timeouts.
// All fields are duration strings ("5s", "200ms", "1h") parsed via
// time.ParseDuration in ToIR.
type ProxyTimeouts struct {
	Dial           string `hcl:"dial,optional"`
	Total          string `hcl:"total,optional"`
	IdleConnection string `hcl:"idle_connection,optional"`
}

// ProxyRetry configures the retry wrapper in the generated backend client.
type ProxyRetry struct {
	MaxAttempts   int    `hcl:"max_attempts,optional"`
	RetryOnStatus []int  `hcl:"retry_on_status,optional"`
	BaseDelay     string `hcl:"base_delay,optional"`
}

// Embed is the embed-mode configuration block.
type Embed struct {
	// TargetMain is the path (relative to --out) of the user's main.go that
	// the DST edit phase will modify. The file must contain the
	// `// mcpgen:hook` marker comment.
	TargetMain string `hcl:"target_main"`
}

// ToolBlock is a single top-level `tool "<name>" { ... }` block. Description
// is optional — when openapi_operation is set the operation's summary
// populates it; ToIR returns a validation error if the tool ends up with
// no description from either source.
type ToolBlock struct {
	Name             string           `hcl:"name,label"`
	Description      string           `hcl:"description,optional"`
	OpenAPIOperation string           `hcl:"openapi_operation,optional"`
	Input            *ToolInput       `hcl:"input,block"`
	Backend          *ToolBackendHTTP `hcl:"backend,block"`
}

// ToolInput holds the declared input fields for a tool.
type ToolInput struct {
	Fields []ToolField `hcl:"field,block"`
}

// ToolField describes one input parameter accepted by a tool.
type ToolField struct {
	Name        string   `hcl:"name,label"`
	Type        string   `hcl:"type,optional"` // "string" | "number" | "boolean" | "enum(...)"; required unless filled from OpenAPI
	Required    *bool    `hcl:"required,optional"`
	Description string   `hcl:"description,optional"`
	Enum        []string `hcl:"enum,optional"` // populated for type = "enum"
}

// ToolBackendHTTP is the inline-HTTP proxy backend. The "http" label is
// reserved for forward compatibility with other backend kinds.
type ToolBackendHTTP struct {
	Kind         string               `hcl:"kind,label"` // "http"
	Method       string               `hcl:"method"`
	Path         string               `hcl:"path"`
	PathParams   []ToolBackendParam   `hcl:"path_param,block"`
	QueryParams  []ToolBackendParam   `hcl:"query_param,block"`
	HeaderParams []ToolBackendParam   `hcl:"header_param,block"`
	BodyParams   []ToolBackendParam   `hcl:"body_param,block"` // POST/PUT/PATCH only
	Response     *ToolBackendResponse `hcl:"response,block"`
	OnError      *ToolBackendOnError  `hcl:"on_error,block"`
}

// ToolBackendParam ties an input field name to its placement in the
// generated HTTP call.
type ToolBackendParam struct {
	Name string `hcl:"name,label"`
	From string `hcl:"from"`
}

// ToolBackendResponse configures how the backend response is surfaced to
// the MCP caller.
type ToolBackendResponse struct {
	Type            string `hcl:"type,optional"` // "json" | "text"
	ContentTemplate string `hcl:"content_template,optional"`
}

// ToolBackendOnError maps specific backend status codes to user-facing
// MCP error messages. Strings support a single "%s" placeholder for the
// originating input value.
type ToolBackendOnError struct {
	NotFound string `hcl:"not_found,optional"`
}
