package gen

import (
	"fmt"
	"path/filepath"

	"github.com/donaldgifford/mcp-go-gen/internal/ir"
)

// BuildPlans expands a validated *ir.Spec into the ordered list of files
// the renderer will produce for `--mode new`. The set of templates grows
// per IMPL-0001 phase; right now the function emits the minimum-viable
// tree: go.mod plus the binary entry point.
//
// Phase 3 follow-ups (tool handlers, observability subsystem, bearer auth,
// backend client) extend this same function.
func BuildPlans(spec *ir.Spec) ([]Plan, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec is nil")
	}
	if spec.Server.Name == "" {
		return nil, fmt.Errorf("spec.Server.Name is empty; cannot generate")
	}

	plans := []Plan{
		{
			Path:     "go.mod",
			Template: "go.mod.tmpl",
			Data:     spec,
			GoFormat: false,
		},
		{
			Path:     filepath.Join("cmd", spec.Server.Name, "main.go"),
			Template: "cmd/main.go.tmpl",
			Data:     spec,
			GoFormat: true,
		},
	}
	return plans, nil
}
