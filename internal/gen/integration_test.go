package gen_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
	"github.com/donaldgifford/mcp-go-gen/internal/gen"
	"github.com/donaldgifford/mcp-go-gen/internal/ir"
	"github.com/donaldgifford/mcp-go-gen/internal/scaffold"
)

// TestGenerate_MinimalBearerCompiles renders the minimal_bearer fixture into
// a tempdir and runs `go build ./...` on the output. The test is skipped
// when `go` is not in PATH or when the -short flag is set; it requires
// network access to resolve dependencies.
//
// This is the canonical end-to-end Phase 3 check: every template must
// combine into source that actually compiles.
func TestGenerate_MinimalBearerCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not in PATH: %v", err)
	}

	fixture := filepath.Join("..", "config", "testdata", "hcl", "good", "minimal_bearer.hcl")
	spec := mustSpec(t, fixture)
	out := t.TempDir()

	if err := gen.Render(spec, gen.NewFSWriter(out, false)); err != nil {
		t.Fatalf("Render: %v", err)
	}

	ctx := context.Background()
	if err := scaffold.Tidy(ctx, out, nil); err != nil {
		t.Fatalf("go mod tidy: %v", err)
	}

	goBuild := exec.CommandContext(ctx, "go", "build", "./...")
	goBuild.Dir = out
	if output, err := goBuild.CombinedOutput(); err != nil {
		t.Fatalf("go build failed in %s:\n%s\n---\n%v", out, string(output), err)
	}
}

// TestGenerate_Idempotency renders twice and diffs the outputs, catching
// regressions in the deterministic-order contract BuildPlans depends on.
func TestGenerate_Idempotency(t *testing.T) {
	fixture := filepath.Join("..", "config", "testdata", "hcl", "good", "minimal_bearer.hcl")
	spec := mustSpec(t, fixture)

	first := t.TempDir()
	second := t.TempDir()
	if err := gen.Render(spec, gen.NewFSWriter(first, false)); err != nil {
		t.Fatalf("first Render: %v", err)
	}
	if err := gen.Render(spec, gen.NewFSWriter(second, false)); err != nil {
		t.Fatalf("second Render: %v", err)
	}

	walkErr := filepath.Walk(first, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(first, p)
		a, _ := os.ReadFile(p)
		b, _ := os.ReadFile(filepath.Join(second, rel))
		if !bytes.Equal(a, b) {
			t.Errorf("file %s differs between renders", rel)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
}

func mustSpec(t *testing.T, fixture string) *ir.Spec {
	t.Helper()
	cfg, diags := config.Decode(fixture)
	if diags.HasErrors() {
		t.Fatalf("decode: %s", diags.Error())
	}
	spec, err := config.ToIR(cfg)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}
	return spec
}
