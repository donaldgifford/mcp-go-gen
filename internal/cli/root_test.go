package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootCmd_HelpListsSubcommands(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test", "deadbeef")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("root --help returned error: %v", err)
	}

	help := buf.String()
	for _, name := range []string{"init", "validate", "generate"} {
		if !strings.Contains(help, name) {
			t.Errorf("root help missing subcommand %q; got:\n%s", name, help)
		}
	}
}

func TestRootCmd_VersionFormat(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("1.2.3", "abc123")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--version returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "1.2.3") || !strings.Contains(got, "abc123") {
		t.Errorf("--version output = %q, want both 1.2.3 and abc123", got)
	}
}

func TestRootCmd_VerboseFlagParsedGlobally(t *testing.T) {
	t.Parallel()

	// Use validate on a nonexistent path — guaranteed to fail independently
	// of working-directory state. We only care that --verbose is accepted at
	// the root level and flows down to the subcommand.
	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"--verbose", "validate", "/nonexistent/mcpgen.hcl"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("validate on nonexistent path should fail")
	}
	// The important assertion is that Cobra did not reject --verbose.
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("root --verbose rejected: %v", err)
	}
}

func TestGenerateCmd_UnknownModeRejected(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"generate", "--mode", "bogus", "--out", "/tmp/x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown --mode, got nil")
	}
	if !strings.Contains(err.Error(), "--mode must be one of") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerateCmd_DryRunAndForceBothAllowed(t *testing.T) {
	t.Parallel()

	// Use a nonexistent config so flag parsing succeeds but the run
	// fails at a later step. The assertion here is purely about Cobra
	// accepting --dry-run + --force together; the Decode failure is
	// the first concrete side effect we can observe without a fixture.
	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"generate", "--config", "/nonexistent.hcl", "--mode", "new", "--out", "/tmp/x", "--dry-run", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from nonexistent config")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("flag rejected: %v", err)
	}
}

func TestValidateCmd_GoodHCLExitsZero(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/good.hcl"
	const src = `mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"validate", path})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(stdout.String(), ": ok") {
		t.Errorf("stdout = %q, want contains ': ok'", stdout.String())
	}
}

func TestValidateCmd_BadHCLSurfacesError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/bad.hcl"
	// mcpgen_version missing → gohcl decode error.
	if err := os.WriteFile(path, []byte(`server { name = "x" }`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"validate", path})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("validate bad.hcl succeeded, want error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want contains 'decode'", err)
	}
}

func TestValidateCmd_RequiresPathArg(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"validate"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing <path> arg, got nil")
	}
}

func TestInitCmd_WritesStarterHCL(t *testing.T) {
	// No t.Parallel — t.Chdir is incompatible with parallel tests.

	dir := t.TempDir()
	t.Chdir(dir)

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"init"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(stdout.String(), "wrote mcpgen.hcl") {
		t.Errorf("stdout = %q, want 'wrote mcpgen.hcl'", stdout.String())
	}
}

func TestInitCmd_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Seed an existing file.
	if err := os.WriteFile("mcpgen.hcl", []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"init"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "pass --force") {
		t.Fatalf("err = %v, want overwrite-refusal", err)
	}
}

func TestInitCmd_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile("mcpgen.hcl", []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"init", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --force: %v", err)
	}

	got, err := os.ReadFile("mcpgen.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "existing" {
		t.Error("file not overwritten")
	}
	if !strings.Contains(string(got), "mcpgen_version") {
		t.Errorf("overwritten file missing mcpgen_version marker")
	}
}
