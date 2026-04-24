package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Valid modes for `mcp-go-gen generate --mode`.
const (
	ModeNew   = "new"
	ModeEmbed = "embed"
)

// generateOptions holds the parsed flags for the generate subcommand.
// Broken out so flag parsing can be tested in isolation from Cobra.
type generateOptions struct {
	config string
	mode   string
	out    string
	force  bool
	dryRun bool
}

func (o *generateOptions) validate() error {
	switch o.mode {
	case ModeNew, ModeEmbed:
	default:
		return fmt.Errorf("--mode must be one of [%s, %s], got %q", ModeNew, ModeEmbed, o.mode)
	}
	if o.out == "" {
		return fmt.Errorf("--out is required")
	}
	return nil
}

func newGenerateCmd() *cobra.Command {
	opts := generateOptions{}

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Read the spec and write generated Go code",
		Long: "Reads the HCL spec given by --config and writes generated Go source " +
			"to --out. In --mode new the generator scaffolds a fresh module; in " +
			"--mode embed it writes into an existing module and edits the target main.go.",
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			return opts.validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			loggerFrom(cmd.Context()).Debug("generate invoked",
				"config", opts.config,
				"mode", opts.mode,
				"out", opts.out,
				"force", opts.force,
				"dry_run", opts.dryRun,
			)
			return ErrNotImplemented
		},
	}

	cmd.Flags().StringVar(&opts.config, "config", "mcpgen.hcl",
		"path to the HCL spec")
	cmd.Flags().StringVar(&opts.mode, "mode", ModeNew,
		"output mode (new | embed)")
	cmd.Flags().StringVar(&opts.out, "out", "",
		"output directory (required)")
	cmd.Flags().BoolVar(&opts.force, "force", false,
		"overwrite existing files in --out")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false,
		"print planned writes to stdout without touching disk")

	return cmd
}
