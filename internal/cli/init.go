package cli

import (
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter mcpgen.hcl in the current directory",
		Long: "Writes a minimal mcpgen.hcl spec that describes a single-tool " +
			"proxy server with bearer auth. Use --force to overwrite an existing file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loggerFrom(cmd.Context()).Debug("init invoked", "force", force)
			return ErrNotImplemented
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite an existing mcpgen.hcl in the current directory")

	return cmd
}
