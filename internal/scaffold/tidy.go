package scaffold

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

// Tidy runs `go mod tidy` in dir. It shells out to the `go` binary on PATH;
// a missing `go` surfaces as an actionable error so users know exactly
// what's missing. stderr and stdout are merged into a single combined
// buffer and written to out (usually the CLI logger's stderr) on success.
//
// Per IMPL-0001 resolved-question #5, the generator requires go in PATH
// and fails loudly rather than silently degrading.
func Tidy(ctx context.Context, dir string, out io.Writer) error {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("`go` not found in PATH; mcp-go-gen generate requires Go: %w", err)
	}

	cmd := exec.CommandContext(ctx, goPath, "mod", "tidy") //nolint:gosec // goPath resolved via LookPath above
	cmd.Dir = dir
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy in %s: %w\n%s", dir, err, string(combined))
	}
	if out != nil && len(combined) > 0 {
		if _, writeErr := out.Write(combined); writeErr != nil {
			return fmt.Errorf("write tidy output: %w", writeErr)
		}
	}
	return nil
}
