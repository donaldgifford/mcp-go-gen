package cli

import (
	"io"
	"log/slog"
)

// newLogger returns a slog.Logger that emits JSON to w. It is INFO by default
// and DEBUG when verbose is true.
//
// The generator must never write logs to stdout — stdout is reserved for the
// --dry-run output of `generate` so that it can be piped into other tools.
// Callers pass os.Stderr here; w is a parameter only for testability.
func newLogger(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}
