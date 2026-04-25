package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// loggerKey is the context key under which the CLI attaches its slog.Logger
// during PersistentPreRunE. Subcommand RunE functions retrieve it via
// loggerFrom.
type loggerKey struct{}

// loggerFrom extracts the CLI logger from ctx. It falls back to a discard
// logger so callers never need a nil check — but this should not happen in
// practice because Execute always installs one.
func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}

// Execute builds the root command, attaches subcommands, and runs the CLI.
// It returns a non-nil error if the invoked command fails; Cobra has already
// printed the error to stderr by that point, so main only needs to choose
// an exit code.
//
// version and commit are stamped at build time via ldflags (see Makefile).
func Execute(version, commit string) error {
	root := newRootCmd(version, commit)
	return root.Execute()
}

func newRootCmd(version, commit string) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "mcp-go-gen",
		Short: "Generate a Go MCP server from an HCL2 spec",
		Long: "mcp-go-gen reads an HCL2 configuration describing an MCP server and " +
			"emits Go source code that implements it. See docs/design/0004-mcpgen-generator.md " +
			"for the HCL schema and generated-code layout.",
		Version:       fmt.Sprintf("%s (commit %s)", version, commit),
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger(os.Stderr, verbose)
			ctx := context.WithValue(cmd.Context(), loggerKey{}, logger)
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"enable debug-level logging to stderr")

	cmd.AddCommand(
		newInitCmd(),
		newValidateCmd(),
		newGenerateCmd(),
	)

	return cmd
}
