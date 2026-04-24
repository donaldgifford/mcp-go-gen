package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/donaldgifford/mcp-go-gen/internal/ir"
)

// helper: parse inline HCL and convert; returns the first error message on
// failure.
func loadAndConvert(t *testing.T, src string) (*ir.Spec, error) {
	t.Helper()
	cfg, diags := DecodeBytes([]byte(src), "inline.hcl")
	if diags.HasErrors() {
		t.Fatalf("decode: %s", diags.Error())
	}
	return ToIR(cfg)
}

func TestToIR_MinimalBearerAppliesDefaults(t *testing.T) {
	t.Parallel()

	spec, err := loadAndConvert(t, `
mcpgen_version = "1"
server {
  name = "minimal"
  listener { addr = ":7070" }
  auth {
    bearer {
      tokens_env = "MCP_TOKENS"
    }
  }
}
tool "echo" {
  description = "Echo."
  input {
    field "msg" {
      type = "string"
    }
  }
}
`)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}

	if spec.Server.EndpointPath != "/mcp" {
		t.Errorf("EndpointPath default = %q, want /mcp", spec.Server.EndpointPath)
	}
	if spec.Observability.Logging.Format != "json" {
		t.Errorf("Logging.Format default = %q, want json", spec.Observability.Logging.Format)
	}
	if !spec.Observability.Metrics.Enabled {
		t.Error("Metrics.Enabled default = false, want true")
	}
	if spec.Observability.Metrics.Path != "/metrics" {
		t.Errorf("Metrics.Path default = %q, want /metrics", spec.Observability.Metrics.Path)
	}
	if !spec.Observability.Tracing.Enabled {
		t.Error("Tracing.Enabled default = false, want true")
	}

	bearer, ok := spec.Auth.(ir.AuthBearer)
	if !ok {
		t.Fatalf("Auth type = %T, want ir.AuthBearer", spec.Auth)
	}
	if bearer.TokensEnv != "MCP_TOKENS" {
		t.Errorf("TokensEnv = %q, want MCP_TOKENS", bearer.TokensEnv)
	}

	if len(spec.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(spec.Tools))
	}
	tool := spec.Tools[0]
	if tool.Kind != ir.ToolKindStub {
		t.Errorf("Tool kind = %v, want ToolKindStub", tool.Kind)
	}
}

func TestToIR_AuthExactlyOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		authBlk string
		wantErr string
	}{
		{
			name:    "none_only",
			authBlk: `none {}`,
		},
		{
			name:    "bearer_only",
			authBlk: `bearer { tokens_env = "X" }`,
		},
		{
			name: "two_blocks_rejected",
			authBlk: `none {}
				bearer { tokens_env = "X" }`,
			wantErr: "exactly one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    ` + tt.authBlk + `
  }
}
`
			_, err := loadAndConvert(t, src)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ToIR: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ToIR succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ToIR err = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestToIR_VersionMismatchRejected(t *testing.T) {
	t.Parallel()

	_, err := loadAndConvert(t, `
mcpgen_version = "2"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
`)
	if err == nil {
		t.Fatal("ToIR accepted mcpgen_version=2")
	}
	if !strings.Contains(err.Error(), `"2", want "1"`) {
		t.Errorf("err = %v, want version-mismatch message", err)
	}
}

func TestToIR_OIDCDynamicCacheTTLParsed(t *testing.T) {
	t.Parallel()

	spec, err := loadAndConvert(t, `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    oidc_dynamic {
      issuer    = "https://auth/realms/main"
      audience  = "mcp"
      cache_ttl = "15m"
    }
  }
}
`)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}
	dyn, ok := spec.Auth.(ir.AuthOIDCDynamic)
	if !ok {
		t.Fatalf("Auth type = %T", spec.Auth)
	}
	if dyn.CacheTTL != 15*time.Minute {
		t.Errorf("CacheTTL = %v, want 15m", dyn.CacheTTL)
	}
}

func TestToIR_OIDCDynamicCacheTTLDefault(t *testing.T) {
	t.Parallel()

	spec, err := loadAndConvert(t, `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    oidc_dynamic {
      issuer   = "https://auth/realms/main"
      audience = "mcp"
    }
  }
}
`)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}
	dyn := spec.Auth.(ir.AuthOIDCDynamic)
	if dyn.CacheTTL != time.Hour {
		t.Errorf("default CacheTTL = %v, want 1h", dyn.CacheTTL)
	}
}

func TestToIR_ToolKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolBody string
		wantKind ir.ToolKind
		wantErr  string
	}{
		{
			name: "proxy_inline_http",
			toolBody: `
  description = "x"
  backend "http" {
    method = "GET"
    path   = "/x"
  }
`,
			wantKind: ir.ToolKindProxy,
		},
		{
			name: "stub_no_backend",
			toolBody: `
  description = "x"
`,
			wantKind: ir.ToolKindStub,
		},
		{
			name: "openapi_without_proxy_openapi_spec_rejected",
			toolBody: `
  description       = "x"
  openapi_operation = "getX"
`,
			wantErr: "requires a top-level proxy.openapi.spec",
		},
		{
			name: "both_backend_and_openapi_operation_rejected",
			toolBody: `
  description       = "x"
  openapi_operation = "getX"
  backend "http" {
    method = "GET"
    path   = "/x"
  }
`,
			wantErr: "cannot set both backend and openapi_operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
tool "t" {
` + tt.toolBody + `
}
`
			spec, err := loadAndConvert(t, src)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToIR: %v", err)
			}
			if spec.Tools[0].Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", spec.Tools[0].Kind, tt.wantKind)
			}
		})
	}
}

func TestToIR_DuplicateToolNameRejected(t *testing.T) {
	t.Parallel()

	src := `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
tool "dup" { description = "a" }
tool "dup" { description = "b" }
`
	_, err := loadAndConvert(t, src)
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("err = %v, want duplicate-name error", err)
	}
}

func TestToIR_FieldTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hcl      string
		wantType ir.FieldType
		wantEnum []string
		wantErr  string
	}{
		{name: "default_empty_is_string", hcl: `field "f" {}`, wantType: ir.FieldTypeString},
		{name: "string", hcl: `field "f" { type = "string" }`, wantType: ir.FieldTypeString},
		{name: "number", hcl: `field "f" { type = "number" }`, wantType: ir.FieldTypeNumber},
		{name: "boolean", hcl: `field "f" { type = "boolean" }`, wantType: ir.FieldTypeBoolean},
		{name: "array_string", hcl: `field "f" { type = "[]string" }`, wantType: ir.FieldTypeArrayString},
		{name: "array_number", hcl: `field "f" { type = "[]number" }`, wantType: ir.FieldTypeArrayNumber},
		{name: "array_boolean", hcl: `field "f" { type = "[]boolean" }`, wantType: ir.FieldTypeArrayBoolean},
		{
			name:     "enum_inline",
			hcl:      `field "f" { type = "enum(red,green,blue)" }`,
			wantType: ir.FieldTypeEnum,
			wantEnum: []string{"red", "green", "blue"},
		},
		{
			name: "enum_attr",
			hcl: `field "f" {
      type = "enum"
      enum = ["a", "b"]
    }`,
			wantType: ir.FieldTypeEnum,
			wantEnum: []string{"a", "b"},
		},
		{
			name:    "enum_no_values_rejected",
			hcl:     `field "f" { type = "enum" }`,
			wantErr: "enum requires at least one value",
		},
		{
			name:    "unsupported_nested_object",
			hcl:     `field "f" { type = "object" }`,
			wantErr: `unsupported`,
		},
		{
			name:    "unsupported_array_element",
			hcl:     `field "f" { type = "[]object" }`,
			wantErr: "unsupported array element type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
tool "t" {
  description = "x"
  input {
    ` + tt.hcl + `
  }
}
`
			spec, err := loadAndConvert(t, src)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToIR: %v", err)
			}
			got := spec.Tools[0].Inputs[0]
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if len(tt.wantEnum) > 0 {
				if !slicesEq(got.Enum, tt.wantEnum) {
					t.Errorf("Enum = %v, want %v", got.Enum, tt.wantEnum)
				}
			}
		})
	}
}

func TestToIR_ProxyDurations(t *testing.T) {
	t.Parallel()

	spec, err := loadAndConvert(t, `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
proxy {
  base_url = "https://api"
  timeouts {
    dial            = "5s"
    total           = "30s"
    idle_connection = "90s"
  }
  retry {
    max_attempts    = 3
    retry_on_status = [502, 503]
    base_delay      = "200ms"
  }
}
`)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}
	if spec.Proxy.DialTimeout != 5*time.Second {
		t.Errorf("DialTimeout = %v, want 5s", spec.Proxy.DialTimeout)
	}
	if spec.Proxy.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", spec.Proxy.MaxAttempts)
	}
	if spec.Proxy.RetryBaseDelay != 200*time.Millisecond {
		t.Errorf("RetryBaseDelay = %v, want 200ms", spec.Proxy.RetryBaseDelay)
	}
}

func TestToIR_BadDurationReportsField(t *testing.T) {
	t.Parallel()

	_, err := loadAndConvert(t, `
mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
proxy {
  base_url = "https://api"
  timeouts { dial = "5seconds" }
}
`)
	if err == nil || !strings.Contains(err.Error(), "proxy.timeouts.dial") {
		t.Fatalf("err = %v, want dial-duration error", err)
	}
}

func TestToIR_NilConfigRejected(t *testing.T) {
	t.Parallel()

	_, err := ToIR(nil)
	if err == nil || !errors.Is(err, err) {
		t.Fatal("ToIR(nil) did not return an error")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("err = %v, want nil-mention", err)
	}
}

func slicesEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
