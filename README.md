# mcp-go-gen

An HCL-driven code generator that emits a complete, runnable [Model Context
Protocol](https://modelcontextprotocol.io) server in Go from a declarative
spec. Point it at an existing HTTP API or OpenAPI document, pick an auth
scheme, and get back a buildable module with observability wired in.

## What you get

- A compilable Go module with `cmd/<name>/main.go`, auth middleware,
  observability (slog JSON logs, Prometheus `/metrics`, OTel tracing), and
  one handler per declared tool.
- Two output modes: `--mode new` scaffolds a fresh module; `--mode embed`
  writes helpers into an existing module and idempotently inserts the
  Register call at a `// mcpgen:hook` marker via dave/dst.
- Four auth schemes: `none`, static bearer, OIDC with fixed issuer/JWKS,
  OIDC with dynamic discovery.
- Two tool-input flavors: inline HCL or by reference to an OpenAPI 3.x
  document's `operationId`.
- Idempotent regeneration — byte-identical output for identical input.

## Install

```bash
go install github.com/donaldgifford/mcp-go-gen/cmd/mcp-go-gen@latest
```

Or clone and build:

```bash
git clone https://github.com/donaldgifford/mcp-go-gen
cd mcp-go-gen
make build   # → build/bin/mcp-go-gen
```

Toolchain versions are pinned in `mise.toml` (Go 1.26.1, golangci-lint 2.11.4).
Run `mise install` if you manage your tooling with mise.

## Quickstart

```bash
# 1. Drop a starter mcpgen.hcl in the current directory.
mcp-go-gen init

# 2. Edit the HCL — add tools, pick an auth scheme, point at a backend.
$EDITOR mcpgen.hcl

# 3. Validate before generating.
mcp-go-gen validate mcpgen.hcl

# 4. Generate a new Go module.
mcp-go-gen generate --mode new --out ./demo
cd demo && go run ./cmd/<name>
```

## HCL shape (abridged)

```hcl
mcpgen_version = "1"

server {
  name = "rfc-api-mcp"

  listener {
    addr = ":7070"
  }

  auth {
    bearer {
      tokens_env = "MCP_TOKENS"
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
}

tool "get_rfc" {
  description = "Fetch an RFC by id."

  input {
    field "id" {
      type     = "string"
      required = true
    }
  }

  backend "http" {
    method = "GET"
    path   = "/rfcs/{id}"

    path_param "id" {
      from = "id"
    }

    response {
      type = "json"
    }
  }
}
```

The full schema — including every auth variant, the OpenAPI merge path,
and observability defaults — lives in [`docs/using-mcpgen.md`](docs/using-mcpgen.md).

## Design docs

- [ADR-0001](docs/adr/0001-mcpgen-architecture.md) — architectural decisions
  (two codegen technologies, two output modes, sealed auth sum type).
- [DESIGN-0004](docs/design/0004-mcpgen-generator.md) — detailed design of
  the generator pipeline.
- [IMPL-0001](docs/impl/0001-mcpgen-generator-implementation.md) —
  phase-by-phase implementation tracker.

## Status

The MVP ships all four auth schemes, both output modes, both proxy-input
flavors, and idempotent regeneration under CI. Known gaps tracked in
IMPL-0001 Phase 7 backlog: request-body parameters for OpenAPI
operations, full `Register` body in embed mode, `--allow-missing-operations`
flag.

## Try it locally

A self-contained Docker Compose demo lives at [`demo/`](demo/) — run
`make demo-up` to bring up a generated MCP server + a small Go API +
the official MCP Inspector on a single bridge network. See
[`demo/README.md`](demo/README.md) for the walkthrough.

## Contributing

See [CLAUDE.md](CLAUDE.md) for the repo layout and day-to-day conventions.
`make ci` runs the full pre-merge gate (lint + test + build +
license-check).

## License

Apache-2.0. See [LICENSE](LICENSE).
