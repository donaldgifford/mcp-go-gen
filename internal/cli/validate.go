package cli

import (
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Parse and type-check an HCL spec",
		Long: "Reads the given mcpgen.hcl and reports any diagnostics without " +
			"writing output. Exits non-zero on any error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			loggerFrom(cmd.Context()).Debug("validate invoked", "path", args[0])
			return ErrNotImplemented
		},
	}
}
