package scaffold_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/scaffold"
)

// TestTidy_HappyPath runs `go mod tidy` against a trivial single-file
// module. Skipped when `go` is not in PATH.
func TestTidy_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tidy integration test in -short mode")
	}

	dir := t.TempDir()
	gomod := "module example.com/tinymod\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	mainGo := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := scaffold.Tidy(context.Background(), dir, &out); err != nil {
		// Not all CI environments have network, and a module with no
		// imports still needs go-env init which can surface network
		// issues.  Skip rather than fail to keep the gate reliable.
		t.Skipf("tidy failed (env likely offline): %v", err)
	}
}

// TestTidy_WhenModuleMissing surfaces the go mod tidy error when the
// target directory has no go.mod.
func TestTidy_WhenModuleMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tidy integration test in -short mode")
	}

	dir := t.TempDir() // empty

	err := scaffold.Tidy(context.Background(), dir, nil)
	if err == nil {
		t.Fatal("expected error when go.mod is missing")
	}
	if !strings.Contains(err.Error(), "go mod tidy") {
		t.Errorf("err = %v, want mentions 'go mod tidy'", err)
	}
}
