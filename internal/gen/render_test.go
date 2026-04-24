package gen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/ir"
)

// minimalSpec returns the simplest valid IR the renderer accepts. Used by
// smoke tests that exercise plumbing rather than specific templates.
func minimalSpec() *ir.Spec {
	return &ir.Spec{
		Server: ir.Server{
			Name:         "demo-mcp",
			Version:      "0.0.1",
			ListenerAddr: ":7070",
			EndpointPath: "/mcp",
		},
		Observability: ir.Observability{
			Logging: ir.Logging{Format: "json", Level: "info"},
			Metrics: ir.Metrics{Enabled: true, Path: "/metrics"},
			Tracing: ir.Tracing{Enabled: true, Exporter: "otlp_http"},
		},
		Auth: ir.AuthBearer{TokensEnv: "MCP_TOKENS"},
	}
}

func TestBuildPlans_RejectsNil(t *testing.T) {
	t.Parallel()

	if _, err := BuildPlans(nil); err == nil {
		t.Fatal("BuildPlans(nil) did not return an error")
	}
}

func TestBuildPlans_RejectsEmptyServerName(t *testing.T) {
	t.Parallel()

	spec := minimalSpec()
	spec.Server.Name = ""
	if _, err := BuildPlans(spec); err == nil {
		t.Fatal("BuildPlans(empty name) did not return an error")
	}
}

func TestBuildPlans_EmitsGoModAndMain(t *testing.T) {
	t.Parallel()

	plans, err := BuildPlans(minimalSpec())
	if err != nil {
		t.Fatalf("BuildPlans: %v", err)
	}

	paths := make(map[string]bool, len(plans))
	for _, p := range plans {
		paths[p.Path] = true
	}
	for _, want := range []string{"go.mod", "cmd/demo-mcp/main.go"} {
		if !paths[want] {
			t.Errorf("missing plan for %s; got %v", want, paths)
		}
	}
}

func TestRender_WritesFormattedGoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	w := NewFSWriter(dir, false)

	if err := Render(minimalSpec(), w); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// go.mod is not Go source — stored verbatim.
	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module demo-mcp") {
		t.Errorf("go.mod missing module directive; got %q", goMod)
	}

	// cmd/demo-mcp/main.go must be gofmt-clean and carry the DO NOT EDIT banner.
	mainGo, err := os.ReadFile(filepath.Join(dir, "cmd", "demo-mcp", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.HasPrefix(string(mainGo), generatedHeader) {
		t.Errorf("main.go missing DO NOT EDIT header; got prefix %q", string(mainGo)[:min(50, len(mainGo))])
	}
	if !strings.Contains(string(mainGo), "package main") {
		t.Errorf("main.go missing package clause; got %q", mainGo)
	}
}

func TestRender_Idempotent(t *testing.T) {
	t.Parallel()

	spec := minimalSpec()

	first := t.TempDir()
	second := t.TempDir()
	if err := Render(spec, NewFSWriter(first, false)); err != nil {
		t.Fatalf("first Render: %v", err)
	}
	if err := Render(spec, NewFSWriter(second, false)); err != nil {
		t.Fatalf("second Render: %v", err)
	}

	if err := diffDirs(t, first, second); err != nil {
		t.Fatalf("idempotency diff: %v", err)
	}
}

func TestFSWriter_RefusesNonEmptyDirWithoutForce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stale"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWriter(dir, false)
	if err := w.Write("a", []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	err := w.Commit()
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Commit err = %v, want refuse-non-empty error", err)
	}
}

func TestFSWriter_ForceOverwrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewFSWriter(dir, true)
	if err := w.Write("a", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("file = %q, want %q", got, "new")
	}
}

func TestDryRunWriter_PrintsPaths(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewDryRunWriter(&buf)
	_ = w.Write("z/second", []byte("1234"))
	_ = w.Write("a/first", []byte("12"))
	if err := w.Commit(); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Alphabetical ordering.
	if !strings.Contains(got, "a/first") || !strings.Contains(got, "z/second") {
		t.Errorf("got %q, want both paths listed", got)
	}
	if !strings.HasPrefix(got, "a/first") {
		t.Errorf("dry-run output not sorted; got:\n%s", got)
	}
}

func TestGoIdent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"get_rfc", "GetRfc"},
		{"list-deliveries", "ListDeliveries"},
		{"echo", "Echo"},
		{"", ""},
		{"multi_word_tool_name", "MultiWordToolName"},
	}
	for _, tt := range tests {
		if got := goIdent(tt.in); got != tt.want {
			t.Errorf("goIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// diffDirs walks a and b recursively and fails the test if any file differs.
func diffDirs(t *testing.T, a, b string) error {
	t.Helper()

	return filepath.Walk(a, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(a, path)
		if relErr != nil {
			return relErr
		}
		aContent, aErr := os.ReadFile(path)
		if aErr != nil {
			return aErr
		}
		bContent, bErr := os.ReadFile(filepath.Join(b, rel))
		if bErr != nil {
			return bErr
		}
		if !bytes.Equal(aContent, bContent) {
			return &diffError{path: rel}
		}
		return nil
	})
}

type diffError struct{ path string }

func (e *diffError) Error() string { return "content differs at " + e.path }
