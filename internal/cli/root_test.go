package cli

import (
	"bytes"
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

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"--verbose", "init"})
	// Silence the stub subcommand's printed error.
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	// ErrNotImplemented is expected from init; we're only checking that the
	// --verbose flag is accepted at the root level.
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
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

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"generate", "--mode", "new", "--out", "/tmp/x", "--dry-run", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	// Both flags set together should parse fine and only hit ErrNotImplemented.
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
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

func TestInitCmd_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test", "deadbeef")
	cmd.SetArgs([]string{"init"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected ErrNotImplemented from init, got %v", err)
	}
}
