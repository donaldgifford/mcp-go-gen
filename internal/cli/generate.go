package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
	"github.com/donaldgifford/mcp-go-gen/internal/gen"
	"github.com/donaldgifford/mcp-go-gen/internal/ir"
	"github.com/donaldgifford/mcp-go-gen/internal/scaffold"
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
			return runGenerate(cmd, &opts)
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

func runGenerate(cmd *cobra.Command, opts *generateOptions) error {
	logger := loggerFrom(cmd.Context())
	logger.Debug("generate invoked",
		"config", opts.config,
		"mode", opts.mode,
		"out", opts.out,
		"force", opts.force,
		"dry_run", opts.dryRun,
	)

	if opts.mode == ModeEmbed {
		return fmt.Errorf("--mode embed: %w", ErrNotImplemented)
	}

	cfg, diags := config.Decode(opts.config)
	if diags.HasErrors() {
		return fmt.Errorf("decode %s: %w", opts.config, diags)
	}
	spec, err := config.ToIR(cfg)
	if err != nil {
		return fmt.Errorf("validate %s: %w", opts.config, err)
	}

	if _, ok := spec.Auth.(ir.AuthNone); ok {
		if _, werr := fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: auth { none {} } — generated server will not authenticate requests"); werr != nil {
			return fmt.Errorf("stderr: %w", werr)
		}
	}

	writer := resolveWriter(cmd, opts)

	if err := gen.Render(spec, writer); err != nil {
		return fmt.Errorf("render: %w", err)
	}

	if opts.dryRun {
		return nil
	}

	// Copy source HCL into the generated project root verbatim.
	if err := copyHCL(opts.config, opts.out); err != nil {
		return fmt.Errorf("copy spec: %w", err)
	}

	// Tidy the generated module so its go.sum is populated and the tree
	// is immediately buildable.
	if err := scaffold.Tidy(cmd.Context(), opts.out, cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("tidy: %w", err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "generated %s → %s\n", opts.config, opts.out); err != nil {
		return fmt.Errorf("stdout: %w", err)
	}
	return nil
}

func resolveWriter(cmd *cobra.Command, opts *generateOptions) gen.Writer {
	if opts.dryRun {
		return gen.NewDryRunWriter(cmd.OutOrStdout())
	}
	return gen.NewFSWriter(opts.out, opts.force)
}

// copyHCL writes the source spec into the generated project root verbatim.
// The `--config` and `--out` flags are user input by design for a CLI tool;
// gosec's path-traversal warning is expected and silenced at the call
// site where the taint originates.
func copyHCL(src, destDir string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	dest := filepath.Join(destDir, "mcpgen.hcl")
	if err := os.WriteFile(dest, data, 0o644); err != nil { //nolint:gosec // dest is --out plus a known filename
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
