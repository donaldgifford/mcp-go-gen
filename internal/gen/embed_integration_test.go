package gen_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
	mcpdst "github.com/donaldgifford/mcp-go-gen/internal/dst"
	"github.com/donaldgifford/mcp-go-gen/internal/gen"
	"github.com/donaldgifford/mcp-go-gen/internal/scaffold"
)

// TestEmbed_RendersAndEditsMain generates the embed-mode subset into a
// synthetic user module whose main.go already contains `// mcpgen:hook`,
// then runs go build to prove the result compiles. Idempotency is
// verified by running the whole flow a second time and asserting no
// file changed.
func TestEmbed_RendersAndEditsMain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not in PATH: %v", err)
	}

	userModule := t.TempDir()
	const modulePath = "example.com/userapp"

	writeFile(t, filepath.Join(userModule, "go.mod"),
		"module "+modulePath+"\n\ngo 1.26.1\n")

	const userMain = `package main

import (
	"context"
	"log"
)

func main() {
	ctx := context.Background()
	app := newApp()
	cfg := loadConfig()

	// mcpgen:hook

	_ = ctx
	_ = app
	_ = cfg
	log.Println("ready")
}

func newApp() int    { return 0 }
func loadConfig() int { return 0 }
`
	cmdDir := filepath.Join(userModule, "cmd", "svc")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetMain := filepath.Join(cmdDir, "main.go")
	writeFile(t, targetMain, userMain)

	const embedHCL = `mcpgen_version = "1"

server {
  name = "svc"

  listener {
    addr = ":7070"
  }

  auth {
    none {}
  }
}

embed {
  target_main = "cmd/svc/main.go"
}

tool "ping" {
  description = "stub"

  input {
    field "message" {
      type     = "string"
      required = true
    }
  }
}
`
	hclPath := filepath.Join(userModule, "mcpgen.hcl")
	writeFile(t, hclPath, embedHCL)

	cfg, diags := config.Decode(hclPath)
	if diags.HasErrors() {
		t.Fatalf("decode: %s", diags.Error())
	}
	spec, err := config.ToIR(cfg)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}

	mp, err := scaffold.ModulePath(userModule)
	if err != nil {
		t.Fatalf("ModulePath: %v", err)
	}
	if mp != modulePath {
		t.Fatalf("ModulePath = %q, want %q", mp, modulePath)
	}
	spec.ModulePath = mp

	plans, err := gen.BuildPlansEmbed(spec, false)
	if err != nil {
		t.Fatalf("BuildPlansEmbed: %v", err)
	}
	for _, p := range plans {
		// New-mode-only files must never land in embed output.
		switch p.Path {
		case "go.mod", "Makefile", "Dockerfile":
			t.Errorf("embed plans include new-mode-only file %s", p.Path)
		}
		if strings.HasPrefix(p.Path, filepath.Join("cmd", "svc")+string(filepath.Separator)) {
			t.Errorf("embed plans include cmd/svc/* path %s", p.Path)
		}
	}

	if err := gen.RenderPlans(plans, gen.NewFSWriter(userModule, true)); err != nil {
		t.Fatalf("RenderPlans: %v", err)
	}

	src, err := os.ReadFile(targetMain)
	if err != nil {
		t.Fatal(err)
	}
	res, err := mcpdst.Edit(src, modulePath+"/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatalf("dst.Edit: %v", err)
	}
	if err := os.WriteFile(targetMain, res.Source, 0o644); err != nil {
		t.Fatal(err)
	}

	// Second pass: idempotency — no file on disk should change.
	if err := gen.RenderPlans(plans, gen.NewFSWriter(userModule, true)); err != nil {
		t.Fatalf("second RenderPlans: %v", err)
	}
	src2, _ := os.ReadFile(targetMain)
	res2, err := mcpdst.Edit(src2, modulePath+"/internal/mcpserver", "mcpserver")
	if err != nil {
		t.Fatalf("second dst.Edit: %v", err)
	}
	if res2.Changed {
		t.Errorf("second dst.Edit reported Changed=true; edit is not idempotent")
	}
	if !bytes.Equal(src2, res2.Source) {
		t.Errorf("second Edit produced different source")
	}

	ctx := context.Background()
	if err := scaffold.Tidy(ctx, userModule, nil); err != nil {
		t.Fatalf("go mod tidy: %v", err)
	}
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = userModule
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed in %s:\n%s\n---\n%v", userModule, string(output), err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
