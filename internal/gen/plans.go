package gen

import (
	"fmt"
	"path/filepath"

	"github.com/donaldgifford/mcp-go-gen/internal/ir"
)

// BuildPlans expands a validated *ir.Spec into the ordered list of files
// the renderer will produce for `--mode new`. Keep this function as the
// single source of truth for "what does the new-mode output contain" —
// the renderer never short-circuits it, the CLI never inserts files
// behind its back.
//
// For any spec, BuildPlans is deterministic: same input → same plan list.
// Idempotency of the downstream render depends on this invariant.
func BuildPlans(spec *ir.Spec) ([]Plan, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec is nil")
	}
	if spec.Server.Name == "" {
		return nil, fmt.Errorf("spec.Server.Name is empty; cannot generate")
	}
	authTmpl, err := authTemplate(spec.Auth)
	if err != nil {
		return nil, err
	}

	plans := []Plan{
		{Path: "go.mod", Template: "go.mod.tmpl", Data: spec},
		{Path: "Makefile", Template: "Makefile.tmpl", Data: spec},
		{Path: "Dockerfile", Template: "Dockerfile.tmpl", Data: spec},
		{
			Path:     filepath.Join("cmd", spec.Server.Name, "main.go"),
			Template: "cmd/main.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "observability", "logging.go"),
			Template: "internal/observability/logging.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "observability", "metrics.go"),
			Template: "internal/observability/metrics.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "observability", "tracing.go"),
			Template: "internal/observability/tracing.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "mcpauth", "auth.go"),
			Template: authTmpl,
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "mcpserver", "server.go"),
			Template: "internal/mcpserver/server.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
		{
			Path:     filepath.Join("internal", "mcpserver", "tools.go"),
			Template: "internal/mcpserver/tools.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
	}

	if spec.Proxy != nil {
		plans = append(plans, Plan{
			Path:     filepath.Join("internal", "mcpserver", "backend.go"),
			Template: "internal/mcpserver/backend.go.tmpl",
			Data:     spec,
			GoFormat: true,
		})
	}

	return plans, nil
}

// authTemplate maps each AuthSpec variant to the template that renders
// the generated mcpauth package. The sealed-sum-type switch ensures a
// missing case surfaces as a compile-time warning via exhaustiveness
// linting (and a clear runtime error if one is added without handling).
func authTemplate(a ir.AuthSpec) (string, error) {
	switch a.(type) {
	case ir.AuthNone:
		return "internal/mcpauth/auth_none.go.tmpl", nil
	case ir.AuthBearer:
		return "internal/mcpauth/auth_bearer.go.tmpl", nil
	case ir.AuthOIDC:
		return "internal/mcpauth/auth_oidc.go.tmpl", nil
	case ir.AuthOIDCDynamic:
		return "internal/mcpauth/auth_oidc_dynamic.go.tmpl", nil
	default:
		return "", fmt.Errorf("unknown auth variant %T", a)
	}
}
