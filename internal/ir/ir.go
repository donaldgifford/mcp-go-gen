// Package ir holds the intermediate representation the template renderer
// consumes. The IR is the single contract between the HCL decoder and the
// codegen pipeline; template code in internal/gen/templates never sees
// config.Config.
package ir

import "time"

// Spec is the root of a fully-resolved mcpgen specification. Every field
// here must already be validated, defaulted, and (for OpenAPI-sourced tools)
// enriched — the renderer does not re-check.
type Spec struct {
	Server        Server
	Auth          AuthSpec
	Observability Observability
	Proxy         *ProxySpec // nil for pure-embed specs with no proxy tools
	Embed         *EmbedSpec // non-nil only for --mode embed generation
	Tools         []Tool

	// ModulePath is the Go import path of the module the generated
	// package belongs to. ToIR sets this to Server.Name (the new-mode
	// default). The embed-mode CLI overwrites it with the user module's
	// path before calling Render.
	ModulePath string
}

// Server captures the identity and network surface of the generated MCP
// server. Defaults have been applied in ToIR; optional fields here are
// already populated with concrete values.
type Server struct {
	Name         string
	Version      string
	ListenerAddr string
	EndpointPath string
}

// Observability is the realized version of the HCL observability block.
// Nil pointers in the HCL become defaulted structs here.
type Observability struct {
	Logging Logging
	Metrics Metrics
	Tracing Tracing
}

// Logging is the resolved slog configuration.
type Logging struct {
	Format string // "json" | "text"
	Level  string // "debug" | "info" | "warn" | "error"
}

// Metrics is the resolved /metrics configuration. An empty Addr means the
// generated code mounts /metrics on the primary listener's mux; a non-empty
// Addr means the generated code starts a second http.Server on that addr.
type Metrics struct {
	Enabled bool
	Path    string
	Addr    string
}

// Tracing is the resolved OTel configuration. When Enabled is false the
// generator wires a no-op provider; the remaining fields are still populated
// so the generated template is uniform across both paths.
type Tracing struct {
	Enabled     bool
	ServiceName string
	SampleRatio float64
	Exporter    string
	Endpoint    string
}

// ProxySpec is the resolved top-level proxy block. Durations are already
// parsed. OpenAPISpecPath is empty when no tool uses openapi_operation.
type ProxySpec struct {
	BaseURL         string
	Bearer          *ProxyBearer
	OpenAPISpecPath string
	DialTimeout     time.Duration
	TotalTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxAttempts     int
	RetryOnStatus   []int
	RetryBaseDelay  time.Duration
}

// ProxyBearer carries the env var name the generated backend client reads
// for its outbound bearer token.
type ProxyBearer struct {
	TokenEnv string
}

// EmbedSpec carries the embed-mode configuration. It is non-nil only when
// the HCL includes an `embed { ... }` block.
type EmbedSpec struct {
	TargetMain string
}

// ModulePath returns the Go import path of the module being generated
// into. In `new` mode it matches the server name (the generator's own
// go.mod declares `module <server.name>`). In `embed` mode the CLI
// overwrites this to the existing module's path, read from the user's
// go.mod.
//
// Templates should import helper packages via `<ModulePath>/internal/...`
// rather than `<Server.Name>/internal/...` so embed mode works at all.

// ToolKind distinguishes how the generator wires a tool's body.
type ToolKind int

const (
	// ToolKindProxy emits a handler that calls the generated HTTP backend
	// client. Backend must be non-nil.
	ToolKindProxy ToolKind = iota + 1
	// ToolKindStub emits a handler that delegates to a user-written
	// ServiceFunc_<Name> function in the hand-written service package.
	ToolKindStub
)

// Tool is a single MCP tool definition, post-validation and post-OpenAPI
// merge. Input fields are guaranteed to have a resolved Type and Required.
type Tool struct {
	Name        string
	Description string
	Inputs      []Field
	Kind        ToolKind
	Backend     *HTTPBackend // non-nil when Kind == ToolKindProxy
}

// FieldType enumerates the primitive input types mcpgen v1 supports.
type FieldType int

// FieldType values. v1 supports the flat primitives plus flat arrays of
// them; nested objects are out of scope per DESIGN-0004 non-goals.
const (
	FieldTypeString FieldType = iota + 1
	FieldTypeNumber
	FieldTypeBoolean
	FieldTypeEnum
	FieldTypeArrayString
	FieldTypeArrayNumber
	FieldTypeArrayBoolean
)

// Field describes one tool input. Enum values are non-empty iff Type ==
// FieldTypeEnum.
type Field struct {
	Name        string
	Type        FieldType
	Required    bool
	Description string
	Enum        []string
}

// HTTPBackend is the resolved inline-HTTP backend for a proxy tool. Params
// are already flattened into ordered slices.
type HTTPBackend struct {
	Method       string
	Path         string
	PathParams   []BackendParam
	QueryParams  []BackendParam
	HeaderParams []BackendParam
	Response     BackendResponse
	OnError      BackendOnError
}

// BackendParam maps an HTTP parameter position (path/query/header) to the
// input field that fills it.
type BackendParam struct {
	Name string
	From string
}

// BackendResponse describes how the upstream response is surfaced to the
// MCP caller. Type is already validated against {"json", "text"}.
type BackendResponse struct {
	Type            string
	ContentTemplate string
}

// BackendOnError holds the status-code-to-message mapping for user-facing
// error overrides. An empty string means "use the upstream error verbatim".
type BackendOnError struct {
	NotFound string
}
