package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Parse and type-check an HCL spec",
		Long: "Reads the given mcpgen.hcl and reports any diagnostics without " +
			"writing output. Exits non-zero on any error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := loggerFrom(cmd.Context())
			path := args[0]
			logger.Debug("validate invoked", "path", path)

			cfg, diags := config.Decode(path)
			if diags.HasErrors() {
				// diags.Error() is already human-readable; Cobra prints the
				// returned error on stderr. Callers who need highlighted
				// ranges can pipe through `config.FormatDiagnostics` but the
				// concise form is the right default for CI logs.
				return fmt.Errorf("decode %s: %w", path, diags)
			}

			if _, err := config.ToIR(cfg); err != nil {
				return fmt.Errorf("validate %s: %w", path, err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: ok\n", path); err != nil {
				return fmt.Errorf("stdout: %w", err)
			}
			return nil
		},
	}
}
