package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/scaffold"
)

func TestModulePath_ReadsDirective(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gomod := "module example.com/widget\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := scaffold.ModulePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/widget" {
		t.Errorf("ModulePath = %q, want %q", got, "example.com/widget")
	}
}

func TestModulePath_WalksUpward(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/parent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := scaffold.ModulePath(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com/parent" {
		t.Errorf("ModulePath = %q, want parent-module match", got)
	}
}

func TestModulePath_ErrorsWhenMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() // empty — no go.mod

	_, err := scaffold.ModulePath(dir)
	if err == nil {
		t.Fatal("expected error when no go.mod is present")
	}
	if !strings.Contains(err.Error(), "no go.mod") {
		t.Errorf("err = %v, want 'no go.mod' mention", err)
	}
}

func TestModulePath_ErrorsWhenGoModMissesDirective(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// valid Go but no `module` line — go wouldn't accept this either, but we
	// want an actionable error rather than a silent miss.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.26.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := scaffold.ModulePath(dir)
	if err == nil {
		t.Fatal("expected error for go.mod with no module directive")
	}
	if !strings.Contains(err.Error(), "no module directive") {
		t.Errorf("err = %v, want 'no module directive' mention", err)
	}
}
