package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/donaldgifford/mcp-go-gen/internal/ir"
)

// Expected schema version. Breaking changes bump this and the generator
// keeps old decoders around behind deprecation warnings.
const schemaVersion = "1"

// Default values applied in ToIR when the HCL leaves them unset. Keep these
// in sync with DESIGN-0004 §"HCL Schema — Top Level".
const (
	defaultEndpointPath    = "/mcp"
	defaultLoggingFormat   = "json"
	defaultLoggingLevel    = "info"
	defaultMetricsPath     = "/metrics"
	defaultTracingExporter = "otlp_http"
	defaultOIDCCacheTTL    = time.Hour
)

// Backend response type tokens accepted in HCL.
const (
	responseTypeJSON = "json"
	responseTypeText = "text"
)

// ToIR converts a decoded Config into a fully-resolved *ir.Spec, applying
// defaults and running every cross-field validation rule that gohcl cannot
// express through struct tags.
//
// On failure ToIR returns a joined error containing one line per rule
// violation — callers can print the result verbatim or iterate via
// errors.As to recover individual rule failures if needed.
func ToIR(cfg *Config) (*ir.Spec, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}

	var errs []error

	if cfg.Version != schemaVersion {
		errs = append(errs, fmt.Errorf(
			"mcpgen_version = %q, want %q (future breaking changes will bump this; there is no v%s decoder yet)",
			cfg.Version, schemaVersion, cfg.Version))
	}

	spec := &ir.Spec{
		Server:        convertServer(&cfg.Server),
		Observability: convertObservability(cfg.Server.Observability),
	}

	auth, authErr := convertAuth(cfg.Server.Auth)
	if authErr != nil {
		errs = append(errs, authErr)
	}
	spec.Auth = auth

	if cfg.Proxy != nil {
		proxy, err := convertProxy(cfg.Proxy)
		if err != nil {
			errs = append(errs, err)
		}
		spec.Proxy = proxy
	}

	if cfg.Embed != nil {
		spec.Embed = &ir.EmbedSpec{TargetMain: cfg.Embed.TargetMain}
	}

	tools, toolErrs := convertTools(cfg.Tools, cfg.Proxy)
	errs = append(errs, toolErrs...)
	spec.Tools = tools

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return spec, nil
}

func convertServer(s *Server) ir.Server {
	endpoint := s.Listener.EndpointPath
	if endpoint == "" {
		endpoint = defaultEndpointPath
	}
	return ir.Server{
		Name:         s.Name,
		Version:      s.Version,
		ListenerAddr: s.Listener.Addr,
		EndpointPath: endpoint,
	}
}

func convertObservability(o *Observability) ir.Observability {
	obs := ir.Observability{
		Logging: ir.Logging{Format: defaultLoggingFormat, Level: defaultLoggingLevel},
		Metrics: ir.Metrics{Enabled: true, Path: defaultMetricsPath},
		Tracing: ir.Tracing{Enabled: true, Exporter: defaultTracingExporter},
	}
	if o == nil {
		return obs
	}
	if o.Logging != nil {
		if o.Logging.Format != "" {
			obs.Logging.Format = o.Logging.Format
		}
		if o.Logging.Level != "" {
			obs.Logging.Level = o.Logging.Level
		}
	}
	if o.Metrics != nil {
		if o.Metrics.Enabled != nil {
			obs.Metrics.Enabled = *o.Metrics.Enabled
		}
		if o.Metrics.Path != "" {
			obs.Metrics.Path = o.Metrics.Path
		}
		obs.Metrics.Addr = o.Metrics.Addr
	}
	if o.Tracing != nil {
		if o.Tracing.Enabled != nil {
			obs.Tracing.Enabled = *o.Tracing.Enabled
		}
		obs.Tracing.ServiceName = o.Tracing.ServiceName
		obs.Tracing.SampleRatio = o.Tracing.SampleRatio
		if o.Tracing.Exporter != "" {
			obs.Tracing.Exporter = o.Tracing.Exporter
		}
		obs.Tracing.Endpoint = o.Tracing.Endpoint
	}
	return obs
}

// convertAuth enforces the "exactly one of none/bearer/oidc/oidc_dynamic"
// contract and maps the populated sub-block to the corresponding IR type.
func convertAuth(a Auth) (ir.AuthSpec, error) {
	var set []string
	var spec ir.AuthSpec

	if a.None != nil {
		set = append(set, "none")
		spec = ir.AuthNone{}
	}
	if a.Bearer != nil {
		set = append(set, "bearer")
		spec = ir.AuthBearer{
			TokensEnv:    a.Bearer.TokensEnv,
			SubjectClaim: a.Bearer.SubjectClaim,
		}
	}
	if a.OIDC != nil {
		set = append(set, "oidc")
		spec = ir.AuthOIDC{
			Issuer:         a.OIDC.Issuer,
			JWKSURL:        a.OIDC.JWKSURL,
			Audience:       a.OIDC.Audience,
			RequiredScopes: a.OIDC.RequiredScopes,
			SubjectClaim:   a.OIDC.SubjectClaim,
		}
	}
	if a.OIDCDynamic != nil {
		set = append(set, "oidc_dynamic")
		ttl := defaultOIDCCacheTTL
		if a.OIDCDynamic.CacheTTL != "" {
			parsed, err := time.ParseDuration(a.OIDCDynamic.CacheTTL)
			if err != nil {
				return nil, fmt.Errorf("auth.oidc_dynamic.cache_ttl = %q: %w", a.OIDCDynamic.CacheTTL, err)
			}
			ttl = parsed
		}
		spec = ir.AuthOIDCDynamic{
			Issuer:         a.OIDCDynamic.Issuer,
			Audience:       a.OIDCDynamic.Audience,
			RequiredScopes: a.OIDCDynamic.RequiredScopes,
			SubjectClaim:   a.OIDCDynamic.SubjectClaim,
			CacheTTL:       ttl,
		}
	}

	switch len(set) {
	case 0:
		return nil, errors.New("auth block: exactly one of [none, bearer, oidc, oidc_dynamic] must be set, got none")
	case 1:
		return spec, nil
	default:
		return nil, fmt.Errorf("auth block: exactly one of [none, bearer, oidc, oidc_dynamic] must be set, got %d (%s)",
			len(set), strings.Join(set, ", "))
	}
}

func convertProxy(p *Proxy) (*ir.ProxySpec, error) {
	var errs []error

	out := &ir.ProxySpec{
		BaseURL: p.BaseURL,
	}

	if p.Auth != nil && p.Auth.Bearer != nil {
		out.Bearer = &ir.ProxyBearer{TokenEnv: p.Auth.Bearer.TokenEnv}
	}
	if p.OpenAPI != nil {
		out.OpenAPISpecPath = p.OpenAPI.Spec
	}
	if p.Timeouts != nil {
		errs = append(errs, applyTimeouts(out, p.Timeouts)...)
	}
	if p.Retry != nil {
		out.MaxAttempts = p.Retry.MaxAttempts
		out.RetryOnStatus = p.Retry.RetryOnStatus
		d, err := parseOptionalDuration("proxy.retry.base_delay", p.Retry.BaseDelay)
		if err != nil {
			errs = append(errs, err)
		}
		out.RetryBaseDelay = d
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func applyTimeouts(out *ir.ProxySpec, t *ProxyTimeouts) []error {
	type slot struct {
		field string
		raw   string
		dst   *time.Duration
	}
	slots := []slot{
		{"proxy.timeouts.dial", t.Dial, &out.DialTimeout},
		{"proxy.timeouts.total", t.Total, &out.TotalTimeout},
		{"proxy.timeouts.idle_connection", t.IdleConnection, &out.IdleTimeout},
	}
	var errs []error
	for _, s := range slots {
		d, err := parseOptionalDuration(s.field, s.raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		*s.dst = d
	}
	return errs
}

func parseOptionalDuration(field, raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s = %q: %w", field, raw, err)
	}
	return d, nil
}

func convertTools(blocks []ToolBlock, proxy *Proxy) ([]ir.Tool, []error) {
	var errs []error
	tools := make([]ir.Tool, 0, len(blocks))
	seen := make(map[string]struct{}, len(blocks))

	for _, b := range blocks {
		if _, dup := seen[b.Name]; dup {
			errs = append(errs, fmt.Errorf("tool %q: duplicate name", b.Name))
			continue
		}
		seen[b.Name] = struct{}{}

		t, toolErrs := convertTool(b, proxy)
		errs = append(errs, toolErrs...)
		if len(toolErrs) == 0 {
			tools = append(tools, t)
		}
	}
	return tools, errs
}

func convertTool(b ToolBlock, proxy *Proxy) (ir.Tool, []error) {
	var errs []error

	t := ir.Tool{
		Name:        b.Name,
		Description: b.Description,
	}

	hasBackend := b.Backend != nil
	hasOpenAPI := b.OpenAPIOperation != ""

	switch {
	case hasBackend && hasOpenAPI:
		errs = append(errs, fmt.Errorf(
			"tool %q: cannot set both backend and openapi_operation; pick one",
			b.Name))
	case hasBackend:
		t.Kind = ir.ToolKindProxy
		be, beErrs := convertBackend(b.Name, b.Backend)
		errs = append(errs, beErrs...)
		t.Backend = be
	case hasOpenAPI:
		t.Kind = ir.ToolKindProxy
		if proxy == nil || proxy.OpenAPI == nil || proxy.OpenAPI.Spec == "" {
			errs = append(errs, fmt.Errorf(
				"tool %q: openapi_operation = %q requires a top-level proxy.openapi.spec",
				b.Name, b.OpenAPIOperation))
		}
		// The actual merge against the OpenAPI document lands in Phase 5;
		// for now we record the intent and leave Backend nil.
	default:
		t.Kind = ir.ToolKindStub
	}

	if b.Input != nil {
		fields, fieldErrs := convertFields(b.Name, b.Input.Fields)
		errs = append(errs, fieldErrs...)
		t.Inputs = fields
	}

	return t, errs
}

func convertBackend(toolName string, b *ToolBackendHTTP) (*ir.HTTPBackend, []error) {
	var errs []error
	if b.Kind != "http" {
		errs = append(errs, fmt.Errorf("tool %q: backend label %q; only %q is supported", toolName, b.Kind, "http"))
	}

	out := &ir.HTTPBackend{
		Method:       strings.ToUpper(b.Method),
		Path:         b.Path,
		PathParams:   convertParams(b.PathParams),
		QueryParams:  convertParams(b.QueryParams),
		HeaderParams: convertParams(b.HeaderParams),
	}
	if b.Response != nil {
		switch b.Response.Type {
		case "", responseTypeJSON, responseTypeText:
			out.Response.Type = b.Response.Type
			if out.Response.Type == "" {
				out.Response.Type = responseTypeJSON
			}
		default:
			errs = append(errs, fmt.Errorf(
				"tool %q: backend.response.type = %q, want one of [%s, %s]",
				toolName, b.Response.Type, responseTypeJSON, responseTypeText))
		}
		out.Response.ContentTemplate = b.Response.ContentTemplate
	} else {
		out.Response.Type = responseTypeJSON
	}
	if b.OnError != nil {
		out.OnError.NotFound = b.OnError.NotFound
	}
	return out, errs
}

func convertParams(params []ToolBackendParam) []ir.BackendParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]ir.BackendParam, 0, len(params))
	for _, p := range params {
		out = append(out, ir.BackendParam{Name: p.Name, From: p.From})
	}
	return out
}

func convertFields(toolName string, fields []ToolField) ([]ir.Field, []error) {
	var errs []error
	out := make([]ir.Field, 0, len(fields))
	for i := range fields {
		f := &fields[i]
		ft, enum, err := parseFieldType(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("tool %q, field %q: %w", toolName, f.Name, err))
			continue
		}
		required := false
		if f.Required != nil {
			required = *f.Required
		}
		out = append(out, ir.Field{
			Name:        f.Name,
			Type:        ft,
			Required:    required,
			Description: f.Description,
			Enum:        enum,
		})
	}
	return out, errs
}

// parseFieldType maps an HCL field type string to the IR enum. It accepts
// the flat primitive types plus `[]string`, `[]number`, `[]boolean`, and
// `enum(...)` with the values supplied either inline via the HCL enum
// attribute or parenthesized in the type string (e.g. `enum(red,green)`).
func parseFieldType(f *ToolField) (ir.FieldType, []string, error) {
	raw := strings.TrimSpace(f.Type)

	if after, ok := strings.CutPrefix(raw, "[]"); ok {
		return parseArrayType(raw, after)
	}
	if raw == "enum" || strings.HasPrefix(raw, "enum(") {
		return parseEnumType(raw, f.Enum)
	}
	return parsePrimitiveType(raw)
}

func parseArrayType(raw, element string) (ir.FieldType, []string, error) {
	switch strings.TrimSpace(element) {
	case "string":
		return ir.FieldTypeArrayString, nil, nil
	case "number":
		return ir.FieldTypeArrayNumber, nil, nil
	case "boolean":
		return ir.FieldTypeArrayBoolean, nil, nil
	default:
		return 0, nil, fmt.Errorf("type %q: unsupported array element type; v1 allows []string, []number, []boolean", raw)
	}
}

func parseEnumType(raw string, attrValues []string) (ir.FieldType, []string, error) {
	values := attrValues
	if inner, ok := strings.CutPrefix(raw, "enum("); ok {
		inner = strings.TrimSuffix(inner, ")")
		values = splitEnumValues(inner)
	}
	if len(values) == 0 {
		return 0, nil, fmt.Errorf("type = %q: enum requires at least one value", raw)
	}
	return ir.FieldTypeEnum, values, nil
}

func splitEnumValues(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parsePrimitiveType(raw string) (ir.FieldType, []string, error) {
	switch raw {
	case "string", "":
		return ir.FieldTypeString, nil, nil
	case "number":
		return ir.FieldTypeNumber, nil, nil
	case "boolean":
		return ir.FieldTypeBoolean, nil, nil
	default:
		return 0, nil, fmt.Errorf("type %q: unsupported; v1 allows string, number, boolean, enum(...), and flat arrays of these", raw)
	}
}
