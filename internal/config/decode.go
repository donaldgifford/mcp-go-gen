package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// Decode reads and parses an HCL2 file at path and returns the structured
// Config. Diagnostics are preserved on return — callers that only care about
// success/failure can check the second return value for nil.
//
// Decode performs only syntactic and gohcl-level structural checks. Semantic
// rules (exactly-one auth block, tool name uniqueness, version equality)
// live in the ToIR conversion step.
func Decode(path string) (*Config, hcl.Diagnostics) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "read spec",
			Detail:   err.Error(),
			Subject:  &hcl.Range{Filename: path},
		}}
	}
	return DecodeBytes(src, path)
}

// DecodeBytes parses src as HCL2 attributed to filename. It is exposed
// separately from Decode so tests and fuzz targets can feed bytes directly
// without touching the filesystem.
func DecodeBytes(src []byte, filename string) (*Config, hcl.Diagnostics) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, diags
	}

	var cfg Config
	if decodeDiags := gohcl.DecodeBody(file.Body, nil, &cfg); decodeDiags.HasErrors() {
		return nil, decodeDiags
	}

	return &cfg, nil
}

// FormatDiagnostics returns a human-readable rendering of diags suitable
// for printing to stderr. The files map supplies source text for
// error-range highlighting; callers that don't carry one can pass nil.
func FormatDiagnostics(diags hcl.Diagnostics, files map[string]*hcl.File) string {
	if len(diags) == 0 {
		return ""
	}
	var buf bytes.Buffer
	w := hcl.NewDiagnosticTextWriter(&buf, files, 0, true)
	if err := w.WriteDiagnostics(diags); err != nil {
		return fmt.Sprintf("failed to format diagnostics: %v", err)
	}
	return buf.String()
}
