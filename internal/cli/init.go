package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
)

// starterHCL is the scaffold written by `mcp-go-gen init`. It describes a
// proxy-flavored MCP server with bearer auth and a single get_rfc tool —
// enough to run `mcp-go-gen validate mcpgen.hcl` successfully and to see
// every HCL block shape in one place. Adjust Lintingly with the schema in
// internal/config/schema.go.
const starterHCL = `mcpgen_version = "1"

server {
  name    = "my-mcp-server"
  version = "0.1.0"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  observability {
    logging {
      format = "json"
      level  = "info"
    }
    metrics {
      enabled = true
      path    = "/metrics"
      addr    = ":9090"
    }
    tracing {
      enabled      = true
      service_name = "my-mcp-server"
      sample_ratio = 1.0
      exporter     = "otlp_http"
      endpoint     = "http://localhost:4318"
    }
  }

  auth {
    bearer {
      tokens_env    = "MCP_TOKENS"
      subject_claim = "name"
    }
  }
}

proxy {
  base_url = "https://api.example.com"

  auth {
    bearer {
      token_env = "BACKEND_API_TOKEN"
    }
  }

  timeouts {
    dial            = "5s"
    total           = "30s"
    idle_connection = "90s"
  }

  retry {
    max_attempts    = 3
    retry_on_status = [502, 503, 504]
    base_delay      = "200ms"
  }
}

tool "get_rfc" {
  description = "Fetch an RFC by its identifier."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "RFC identifier, e.g. RFC-0042."
    }
  }

  backend "http" {
    method = "GET"
    path   = "/rfcs/{id}"

    path_param "id" {
      from = "id"
    }

    response {
      type             = "json"
      content_template = "RFC {{.id}}: {{.title}}"
    }

    on_error {
      not_found = "RFC %s not found"
    }
  }
}
`

// starterFilename is the filename `init` writes. Kept as a constant so tests
// can assert on it without duplicating the string.
const starterFilename = "mcpgen.hcl"

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter mcpgen.hcl in the current directory",
		Long: "Writes a minimal mcpgen.hcl spec that describes a single-tool " +
			"proxy server with bearer auth. Use --force to overwrite an existing file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := loggerFrom(cmd.Context())
			logger.Debug("init invoked", "force", force)

			if _, err := os.Stat(starterFilename); err == nil {
				if !force {
					return fmt.Errorf("%s already exists; pass --force to overwrite", starterFilename)
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("stat %s: %w", starterFilename, err)
			}

			if err := os.WriteFile(starterFilename, []byte(starterHCL), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", starterFilename, err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", starterFilename); err != nil {
				return fmt.Errorf("stdout: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite an existing mcpgen.hcl in the current directory")

	return cmd
}
