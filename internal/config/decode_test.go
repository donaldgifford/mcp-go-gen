package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecode_GoodFixtures(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("testdata", "hcl", "good")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) = %v", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".hcl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()

			cfg, diags := Decode(path)
			if diags.HasErrors() {
				t.Fatalf("Decode(%s) unexpected errors:\n%s", path, diags.Error())
			}
			if cfg == nil {
				t.Fatalf("Decode(%s) returned nil config", path)
			}
			if cfg.Version == "" {
				t.Errorf("Decode(%s) Version = empty, want non-empty", path)
			}
			if cfg.Server.Name == "" {
				t.Errorf("Decode(%s) Server.Name = empty, want non-empty", path)
			}
		})
	}
}

func TestDecode_BadFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture   string
		errSubstr string
	}{
		{fixture: "missing_version.hcl", errSubstr: "mcpgen_version"},
		{fixture: "missing_auth_block.hcl", errSubstr: "auth"},
		{fixture: "bearer_missing_tokens_env.hcl", errSubstr: "tokens_env"},
		{fixture: "syntax_error.hcl", errSubstr: ""}, // any parse error
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", "hcl", "bad", tt.fixture)
			_, diags := Decode(path)
			if !diags.HasErrors() {
				t.Fatalf("Decode(%s) succeeded, want diagnostics", path)
			}
			if tt.errSubstr != "" && !strings.Contains(diags.Error(), tt.errSubstr) {
				t.Errorf("Decode(%s) diagnostics = %q, want substring %q",
					path, diags.Error(), tt.errSubstr)
			}
		})
	}
}

func TestDecode_MissingFileReturnsDiag(t *testing.T) {
	t.Parallel()

	_, diags := Decode("/nonexistent/path/to/mcpgen.hcl")
	if !diags.HasErrors() {
		t.Fatal("Decode(nonexistent) succeeded, want diag")
	}
}

func TestDecodeBytes_ValidInline(t *testing.T) {
	t.Parallel()

	src := []byte(`
mcpgen_version = "1"
server {
  name = "inline"
  listener {
    addr = ":7070"
  }
  auth {
    none {}
  }
}
`)
	cfg, diags := DecodeBytes(src, "inline.hcl")
	if diags.HasErrors() {
		t.Fatalf("DecodeBytes unexpected errors: %s", diags.Error())
	}
	if cfg.Server.Name != "inline" {
		t.Errorf("Server.Name = %q, want %q", cfg.Server.Name, "inline")
	}
	if cfg.Server.Auth.None == nil {
		t.Error("expected Auth.None to be non-nil")
	}
}

func TestFormatDiagnostics_EmptyReturnsEmpty(t *testing.T) {
	t.Parallel()

	if got := FormatDiagnostics(nil, nil); got != "" {
		t.Errorf("FormatDiagnostics(nil) = %q, want empty string", got)
	}
}
