package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
	mcpdst "github.com/donaldgifford/mcp-go-gen/internal/dst"
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
	config         string
	mode           string
	out            string
	force          bool
	dryRun         bool
	overwriteStubs bool
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
	cmd.Flags().BoolVar(&opts.overwriteStubs, "overwrite-stubs", false,
		"in --mode embed: also regenerate internal/mcpserver/service_stubs.go "+
			"(requires --force)")

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
		"overwrite_stubs", opts.overwriteStubs,
	)

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

	switch opts.mode {
	case ModeNew:
		return runGenerateNew(cmd, opts, spec)
	case ModeEmbed:
		return runGenerateEmbed(cmd, opts, spec)
	default:
		return fmt.Errorf("--mode must be one of [%s, %s], got %q", ModeNew, ModeEmbed, opts.mode)
	}
}

func runGenerateNew(cmd *cobra.Command, opts *generateOptions, spec *ir.Spec) error {
	writer := resolveWriter(cmd, opts)
	if err := gen.Render(spec, writer); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if opts.dryRun {
		return nil
	}
	if err := copyHCL(opts.config, opts.out); err != nil {
		return fmt.Errorf("copy spec: %w", err)
	}
	if err := scaffold.Tidy(cmd.Context(), opts.out, cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("tidy: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "generated %s → %s\n", opts.config, opts.out); err != nil {
		return fmt.Errorf("stdout: %w", err)
	}
	return nil
}

func runGenerateEmbed(cmd *cobra.Command, opts *generateOptions, spec *ir.Spec) error {
	if spec.Embed == nil || spec.Embed.TargetMain == "" {
		return fmt.Errorf("--mode embed requires an `embed { target_main = ... }` block in the HCL spec")
	}
	if opts.overwriteStubs && !opts.force {
		return fmt.Errorf("--overwrite-stubs requires --force (safety: service_stubs.go is hand-written territory)")
	}

	modulePath, err := scaffold.ModulePath(opts.out)
	if err != nil {
		return fmt.Errorf("detect module path: %w", err)
	}
	spec.ModulePath = modulePath

	plans, err := gen.BuildPlansEmbed(spec, opts.overwriteStubs)
	if err != nil {
		return fmt.Errorf("build embed plans: %w", err)
	}
	writer := resolveWriter(cmd, opts)
	if err := gen.RenderPlans(plans, writer); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if opts.dryRun {
		return nil
	}

	targetMain := filepath.Join(opts.out, spec.Embed.TargetMain)
	if err := applyDSTEdit(targetMain, modulePath); err != nil {
		return fmt.Errorf("dst edit %s: %w", targetMain, err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "embedded into %s (module %s)\n", opts.out, modulePath); err != nil {
		return fmt.Errorf("stdout: %w", err)
	}
	return nil
}

// applyDSTEdit reads the user's main.go, applies the idempotent mcpgen
// Register insertion, and writes the result back only when something
// changed. An unchanged file means both the import and the call are
// already present — no-op second generations are silent.
func applyDSTEdit(path, modulePath string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	res, err := mcpdst.Edit(src, modulePath+"/internal/mcpserver", "mcpserver")
	if err != nil {
		return err
	}
	if !res.Changed {
		return nil
	}
	if err := os.WriteFile(path, res.Source, 0o644); err != nil { //nolint:gosec // preserving the user file's address; perms inherited by intent
		return fmt.Errorf("write: %w", err)
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
