---
id: DESIGN-0004
title: "mcpgen — HCL-driven MCP server generator"
status: Draft
author: Donald
created: 2026-04-21
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0004: mcpgen — HCL-driven MCP server generator

**Status:** Draft
**Author:** Donald
**Date:** 2026-04-21

<!--toc:start-->
<!--toc:end-->

## Overview

`mcpgen` is a Go command-line tool that reads an HCL2 configuration
file describing an MCP server and emits Go source code that
implements it. It operates in two output modes — standalone proxy
(new project) and embed (add to existing service) — and supports
the full range of auth schemes we use (none, static bearer, OIDC
/ JWT, dynamic OIDC discovery). Every generated server ships with
slog logging, Prometheus metrics, and OpenTelemetry tracing wired
in by default. The generator uses Go `text/template` for new file
generation and `dave/dst` for structural edits to existing files.

The design choices are captured in `ADR-0001`. This document covers
the implementation: HCL schema, intermediate representation, CLI
surface, generated-code layout, regeneration semantics, and test
strategy.

## Goals and Non-Goals

### Goals

- Read an HCL2 file describing an MCP server and generate a
  compilable, runnable Go service.
- Support both **new project** output (full scaffold with
  `go.mod`, Dockerfile, Makefile) and **embed** output (writes
  into a designated subpath of an existing module and edits the
  target `main.go` to wire it in).
- Support proxy-mode tools whose implementation maps an MCP call
  to an HTTP call against a backend, described either inline in
  HCL or by reference to an OpenAPI 3.x spec.
- Support four auth schemes on the generated MCP listener: none,
  static bearer token, OIDC/JWT with fixed issuer/JWKS, and
  dynamic OIDC discovery via `.well-known/openid-configuration`.
- Ship observability on by default: slog JSON logger,
  `/metrics` Prometheus endpoint, OTLP/HTTP tracing exporter.
- Produce `gofmt`-clean output without requiring a follow-up
  formatting pass.
- Produce `DO NOT EDIT`-headed files for everything it owns,
  plus a clear hand-written scaffold for the bits the user is
  expected to maintain (tool implementations, for example).
- Be idempotent: running `mcpgen generate` twice against the
  same HCL produces byte-identical output.

### Non-Goals

- **Non-HCL input.** YAML/JSON configs are not supported. Users
  who want YAML can `yq` it to HCL or write a wrapper.
- **Non-Go output.** Generating MCP servers in Python, TypeScript,
  or Rust is out of scope. The Bun+Hono walkthrough covers
  TypeScript manually.
- **Runtime reloading of the config.** mcpgen is a build-time tool;
  the generated binary treats its behavior as fixed.
- **Tool business logic generation.** mcpgen generates the tool
  *wiring* — the MCP registration, the argument parsing, the
  observability wrap. For proxy-mode tools it also generates the
  HTTP call. For all other cases, the tool body is a stub that
  calls a user-provided service function. mcpgen does not attempt
  to synthesize business logic.
- **Multi-file OpenAPI specs, OpenAPI 2.0 / Swagger.** OpenAPI 3.0
  and 3.1 are in; 2.0 is not. `$ref` to external files within a
  3.x document is supported; remote `$ref` (HTTP URLs) is not in
  the first version.
- **gRPC / gRPC-gateway input.** Only HTTP/JSON APIs are supported
  as proxy backends in v1.
- **Generating the `webhookd` Phase 3 MCP server.** The Phase 3
  MCP surface is hand-written because it wraps live process state
  (the ring buffer, the executor). mcpgen is better suited to
  CRUD-shaped proxy surfaces. We will evaluate whether a mcpgen
  rewrite is worthwhile after both are in production for a while.

## Background

Three forces push toward a generator:

1. **We have a known-good pattern.** The general Go MCP walkthrough
   (`docs/guide/mcp-server-in-go.md`) captures the shape of every
   MCP server we'd ever write. When the same code shape is going
   to be reproduced multiple times, generating it is strictly
   better than copying it. Hand-written copies drift; generated
   copies are uniform.
2. **The variance is small and declarable.** Across services,
   what actually changes is: server name, tool names, tool
   inputs/outputs, auth scheme, and (for proxy mode) which
   backend each tool calls. All of that fits in an HCL spec of
   reasonable size. The parts that don't vary — middleware
   ordering, signal handling, metric/span instrumentation
   conventions — are exactly the parts that benefit most from
   being generated uniformly.
3. **We already use HCL in related tooling.** `fwsync` uses HCL
   as its config surface; mcpgen's HCL input is consistent with
   that. Engineers switching between the two don't pay a cognitive
   tax.

## Detailed Design

### CLI Surface

```
mcpgen --help
  Generate a Go MCP server from an HCL2 spec.

COMMANDS
  init     Write a starter mcpgen.hcl in the current directory.
  validate Parse and type-check an HCL spec. No output.
  generate Read the spec and write generated Go code.

generate FLAGS
  --config path    HCL file (default: mcpgen.hcl)
  --mode name      new | embed (default: new)
  --out path       Output directory. For new: must not exist or be empty.
                   For embed: existing module root or subpath.
  --force          Overwrite existing files (off by default).
  --dry-run        Print what would be written; don't touch disk.
```

All behavior is driven by the HCL config; CLI flags are just
"where to read from" and "where to write to."

### HCL Schema — Top Level

```hcl
mcpgen_version = "1"

server {
  name    = "rfc-api-mcp"
  version = "1.0.0"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  observability {
    logging {
      format = "json"   # "json" | "text"
      level  = "info"
    }
    metrics {
      enabled = true
      path    = "/metrics"
      addr    = ":9090"  # separate listener; omit to share with /mcp
    }
    tracing {
      enabled       = true
      service_name  = "rfc-api-mcp"
      sample_ratio  = 1.0
      exporter      = "otlp_http"
      endpoint      = "http://localhost:4318"
    }
  }

  auth {
    # exactly one of the blocks below
    bearer { ... }
  }
}

# tools live at top level, each is a labeled block
tool "get_rfc" {
  description = "Fetch an RFC by its identifier."
  input { ... }
  backend { ... }   # proxy mode; optional for embed mode
}
```

`mcpgen_version` at the top is the schema version marker —
breaking changes bump it and we keep old decoders around with
deprecation warnings.

### Auth Blocks

Each auth scheme is a labeled block; exactly one may appear in
`server.auth`.

```hcl
# 1. None — no middleware generated. Explicit opt-in.
auth { none {} }

# 2. Static bearer — map of token to subject name.
auth {
  bearer {
    tokens_env    = "MCP_TOKENS"          # "alice:abc,bob:def"
    subject_claim = "name"                # for log attribution
  }
}

# 3. OIDC / JWT — fixed issuer + JWKS URL.
auth {
  oidc {
    issuer          = "https://auth.internal/realms/main"
    jwks_url        = "https://auth.internal/realms/main/protocol/openid-connect/certs"
    audience        = "mcp-rfc-api"
    required_scopes = ["mcp:read"]
    subject_claim   = "sub"
  }
}

# 4. Dynamic OIDC discovery — issuer only; SDK fetches /.well-known.
auth {
  oidc_dynamic {
    issuer          = "https://auth.internal/realms/main"
    audience        = "mcp-rfc-api"
    required_scopes = ["mcp:read"]
    subject_claim   = "sub"
    cache_ttl       = "1h"
  }
}
```

Generator behavior per scheme:

- **none** emits no auth middleware; logs a warning at generate
  time. A `// WARNING: no authentication configured` comment is
  inserted in the generated file.
- **bearer** emits a static token-map middleware that reads
  `MCP_TOKENS` env var, parses `name:token` pairs, and injects
  the matched subject into context.
- **oidc** emits middleware built on `github.com/coreos/go-oidc/v3`
  with a fixed issuer and JWKS URL. No discovery call at startup.
- **oidc_dynamic** emits middleware that calls
  `oidc.NewProvider(ctx, issuer)` at startup to fetch discovery,
  verifies JWKS per request with a configurable cache TTL.

All schemes produce the same `Subject` type in generated code so
tool handlers remain identical.

### Tool Blocks

Every tool is a labeled block. Two flavors:

**Proxy (inline HTTP):**

```hcl
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

    # parameter mapping: which input -> where in the request
    path_param "id" { from = "id" }

    response {
      type = "json"                        # returns structured content
      content_template = "RFC {{.id}}: {{.title}}"  # optional text
    }

    on_error {
      not_found = "RFC %s not found"       # 404 -> user-facing error
    }
  }
}
```

**Proxy (OpenAPI reference):**

```hcl
tool "get_rfc" {
  description = "Fetch an RFC by its identifier."
  openapi_operation = "getRfcById"

  # optional overrides
  input {
    field "id" {
      description = "RFC identifier, e.g. RFC-0042."  # overrides op description
    }
  }
}
```

The generator resolves `openapi_operation` against the `spec` URL
configured at the proxy block level (see below), pulls parameters
and response shape from there, and treats the rest as a standard
tool.

**Embed (no backend block):**

```hcl
tool "list_deliveries" {
  description = "List recent webhook deliveries."
  input {
    field "limit" { type = "number"; description = "..." }
  }
  # no backend → generator emits a stub that calls
  # ServiceFunc_ListDeliveries(ctx, input), which the user must
  # implement in the hand-written service package.
}
```

### Proxy Block Top-Level Config

For proxy-mode specs, a `proxy` block at the top level configures
the backend connection:

```hcl
proxy {
  base_url = "https://api.example.com"
  auth {
    # how mcpgen authenticates to the backend (separate from
    # how the MCP server authenticates its own callers)
    bearer { token_env = "BACKEND_API_TOKEN" }
  }

  openapi {
    spec = "./api/openapi.yaml"        # only if any tool uses openapi_operation
  }

  timeouts {
    dial             = "5s"
    total            = "30s"
    idle_connection  = "90s"
  }

  retry {
    max_attempts    = 3
    retry_on_status = [502, 503, 504]
    base_delay      = "200ms"
  }
}
```

### Intermediate Representation

HCL decoding produces a `*config.Config`, which is translated to
an IR before codegen:

```go
// internal/ir/ir.go
type Spec struct {
    Server        Server
    Auth          AuthSpec          // interface; one concrete per scheme
    Observability Observability
    Proxy         *ProxySpec        // nil for pure-embed specs
    Tools         []Tool            // already resolved: OpenAPI merged in
}

type Tool struct {
    Name        string
    Description string
    Inputs      []Field
    Kind        ToolKind            // Proxy | Stub
    Backend     *HTTPBackend        // set if Kind == Proxy
    // for Stub, the generator emits:
    //    result, err := svc.Tool_GetRfc(ctx, input)
    // and expects the user to implement svc.Tool_GetRfc.
}

type AuthSpec interface { isAuthSpec() }
type AuthNone   struct{}
type AuthBearer struct { TokensEnv, SubjectClaim string }
type AuthOIDC   struct { Issuer, JWKSURL, Audience string; RequiredScopes []string; SubjectClaim string }
type AuthOIDCDynamic struct { Issuer, Audience string; RequiredScopes []string; SubjectClaim string; CacheTTL time.Duration }

func (AuthNone) isAuthSpec()         {}
func (AuthBearer) isAuthSpec()       {}
func (AuthOIDC) isAuthSpec()         {}
func (AuthOIDCDynamic) isAuthSpec()  {}
```

The IR is the only thing templates see. HCL-specific concerns stop
at the loader.

### Generator Pipeline

```
        HCL file                           OpenAPI (optional)
            │                                     │
            ▼                                     ▼
     hcl/v2 parser                          libopenapi parser
            │                                     │
            ▼                                     ▼
      gohcl.DecodeBody                    operations by opId
            │                                     │
            ▼                                     ▼
       config.Config   ───────►  merge & validate  ◄──── (shared deps & types)
                                       │
                                       ▼
                                    ir.Spec
                                       │
                    ┌──────────────────┼─────────────────┐
                    ▼                  ▼                 ▼
             template renders    DST edits on      new-project
             per-file plans      existing main.go  scaffold (go.mod, ...)
                    │                  │                 │
                    └──────────────────┴─────────────────┘
                                       ▼
                              go/format.Source()
                                       ▼
                                  write to disk
```

Templates live in `internal/gen/templates/` and are embedded via
`//go:embed` at build time. Each template takes a single `ir.Spec`
as its root.

### Generated File Layout

**New-project mode (`--mode new --out ./rfc-api-mcp`):**

```
rfc-api-mcp/
├── go.mod                        # generated
├── go.sum                        # generated via `go mod tidy` at end
├── Makefile                      # generated
├── Dockerfile                    # generated
├── mcpgen.hcl                    # copy of the input
├── cmd/rfc-api-mcp/
│   └── main.go                   # generated; DO NOT EDIT
└── internal/
    ├── config/
    │   └── config.go             # generated; DO NOT EDIT
    ├── observability/
    │   ├── logging.go            # generated; DO NOT EDIT
    │   ├── metrics.go            # generated; DO NOT EDIT
    │   └── tracing.go            # generated; DO NOT EDIT
    ├── mcpauth/
    │   └── auth.go               # generated; DO NOT EDIT
    ├── mcpserver/
    │   ├── server.go             # generated; DO NOT EDIT
    │   └── tools.go              # generated; DO NOT EDIT
    └── backend/                  # proxy mode only
        └── client.go             # generated; DO NOT EDIT
```

For embed-mode tools that use stubs (no backend), the generator
additionally creates `internal/mcpserver/service_stubs.go` which
contains empty functions for the user to implement.

**Embed mode (`--mode embed --out ./internal/mcp`):**

Only the `internal/mcpserver/`, `internal/mcpauth/`, and relevant
observability files are generated. The user's existing
`cmd/<svc>/main.go` is edited via DST to:

1. Insert imports for the generated package.
2. Call `mcpserver.Register(ctx, config, cfg.MCP)` at the marker
   comment `// mcpgen:hook` within `main()`.

If the marker comment is missing, the generator emits a hard
error with instructions — it does not guess where to insert.

### DST Edit for Embed Mode

Example target `main.go` before generation:

```go
func main() {
    ctx := context.Background()
    cfg := config.MustLoad()
    app := hono.NewRouter()
    registerRoutes(app, cfg)

    // mcpgen:hook
    // (mcpgen edits inject the registration call here)

    if err := http.ListenAndServe(":8080", app); err != nil {
        log.Fatal(err)
    }
}
```

After generation:

```go
import (
    // ...existing imports
    "myorg/internal/mcpserver"
)

func main() {
    ctx := context.Background()
    cfg := config.MustLoad()
    app := hono.NewRouter()
    registerRoutes(app, cfg)

    // mcpgen:hook
    if err := mcpserver.Register(ctx, app, cfg); err != nil {
        log.Fatalf("mcp register: %v", err)
    }

    if err := http.ListenAndServe(":8080", app); err != nil {
        log.Fatal(err)
    }
}
```

DST preserves the exact formatting of the surrounding code.

### Regeneration Semantics

- Generated files carry the standard `// Code generated by mcpgen.
  DO NOT EDIT.` header.
- Regenerating overwrites these files unconditionally.
- Hand-written files (`service_stubs.go` if the user has edited
  it, their own service package) are never touched.
- The embed-mode `main.go` edit is idempotent: mcpgen looks for the
  `// mcpgen:hook` marker and replaces the single line
  immediately following it. If that line is absent or already has
  the expected call, the generator is a no-op on that file.

### Observability in Generated Code

Every generated tool handler follows the template from
`docs/guide/mcp-server-in-go.md` §11:

```go
func (t *Tools) GetRfc(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    start := time.Now()
    subj, _ := mcpauth.SubjectFromContext(ctx)

    ctx, span := t.tracer.Start(ctx, "mcp.tool.get_rfc",
        trace.WithAttributes(attribute.String("mcp.subject", subj.Name)))
    defer span.End()

    id, err := req.RequireString("id")
    if err != nil {
        t.recordOutcome("get_rfc", subj.Name, "bad_input", start)
        return mcp.NewToolResultError(err.Error()), nil
    }

    // === Proxy mode: generated backend call ===
    result, err := t.backend.GetRfc(ctx, id)
    // === Embed mode: generated service call ===
    // result, err := t.svc.GetRfc(ctx, id)

    if err != nil {
        span.RecordError(err)
        t.recordOutcome("get_rfc", subj.Name, "error", start)
        return mcp.NewToolResultError(err.Error()), nil
    }

    t.recordOutcome("get_rfc", subj.Name, "success", start)
    slog.InfoContext(ctx, "mcp tool completed",
        "tool", "get_rfc", "mcp.subject", subj.Name,
        "outcome", "success",
        "duration_ms", time.Since(start).Milliseconds())
    return mcp.NewToolResultStructured(result), nil
}
```

Metrics:

```
mcp_tool_invocations_total{tool, subject, outcome}
mcp_tool_duration_seconds{tool, outcome}
mcp_auth_failures_total{reason}
# proxy mode only:
mcp_backend_requests_total{tool, status}
mcp_backend_request_duration_seconds{tool, status}
```

Spans:

```
mcp.tool.<tool_name>     (always)
mcp.backend.<tool_name>  (proxy mode only; child of the tool span)
```

Log correlation uses the same `traceHandler` slog wrapper pattern
from webhookd Phase 1 — `trace_id` and `span_id` stamped on every
log line emitted with a live context.

## API / Interface Changes

- New binary `mcpgen` published in our internal tooling
  distribution.
- New HCL schema version `"1"` established; future breaking
  changes bump this and are opt-in per-repo.
- No existing-service API changes; this is a new tool.

## Data Model

mcpgen is stateless. Each invocation reads inputs, writes outputs,
exits. No database, no cache beyond in-memory during a single run.

The generated services are as stateful as their HCL specifies —
i.e., mostly stateless for proxy mode, dependent on user service
packages for embed mode.

## Testing Strategy

**Unit tests** on the generator itself:

- `internal/config` — HCL decoding tests with good-case and
  error-case fixtures; HCL error diagnostics assert specific
  ranges.
- `internal/ir` — conversion from decoded config to IR, including
  OpenAPI merging, validation of required fields, duplicate tool
  detection.
- `internal/gen` — template rendering tests using golden files:
  for a known input IR, the generated output is byte-compared to
  a committed fixture. `go test -update` regenerates fixtures
  when a template changes.
- `internal/dst` — embed-mode edits applied to canned `main.go`
  fixtures; assert resulting source compiles (via
  `go/parser.ParseFile`).

**Integration tests:**

- Generate a new project from each combination of `{new,embed} ×
  {none, bearer, oidc, oidc_dynamic} × {proxy-inline, proxy-
  openapi, embed-stub}`, run `go build` and `go test ./...` on
  the output. Done in CI via a test matrix; slow but it's the
  thing that actually proves the generator works.
- Spin up the generated server on a random port, hit it with the
  MCP Inspector SDK client, assert tool discovery and basic
  invocation.

**Fuzz target:** `FuzzHCLDecode` on the config parser, to
exercise malformed HCL and ensure diagnostics rather than panics.

**Regeneration test:** generate once, generate again with the same
inputs, assert byte-identical output.

## Migration / Rollout Plan

1. **Week 1–2:** Land the generator binary with `new` mode,
   `bearer` auth, and inline-HCL proxy. Minimum viable.
2. **Week 3:** Add OIDC and OIDC-dynamic auth schemes. Add the
   `none` auth scheme with the generator-level warning.
3. **Week 4:** Add OpenAPI input path. Wire up `libopenapi`.
4. **Week 5:** Add `embed` mode with DST edits. This is the
   highest-risk change because it touches real user code; gate
   rollout with heavy test coverage.
5. **Week 6:** Dogfood. Generate an MCP frontend for the markdown
   RFC API. Compare against a hand-written equivalent.
6. **Week 7+:** Publish internally. Add it to the Backstage
   templates so service creation has an "MCP server" option.

Rollback: the generator is a build-time tool. If a generated
service has a bug, users regenerate after a patched mcpgen release
(or pin to an older version via `go install <pkg>@<ver>`).

## Open Questions

- **Tool-level auth scoping.** Some tools are read-only; others
  perform writes. Do we support per-tool required scopes in HCL
  (e.g., `get_rfc { required_scopes = ["mcp:read"] }`) that
  extend the server-level defaults? Probably yes, v1.1. For v1,
  all tools inherit server-level scopes.
- **Input type system.** v1 supports `string`, `number`,
  `boolean`, `enum(...)`, and flat arrays of those. Nested
  objects are not supported in tool inputs because they confuse
  LLMs; this is by design but worth re-examining if users
  complain. The escape hatch today is to inline a JSON string
  and parse it in the handler.
- **Tool output mapping from OpenAPI responses.** If a schema's
  response has 30 fields and we want the LLM to see only 5, how
  do users declare that? Candidate: `output { select = ["id",
  "title", "status"] }`. Deferred to v1.1 because the v1
  behavior (return the whole response) is correct if not optimal.
- **Handling of paginated backend endpoints.** v1 generates the
  single-page call; the LLM handles pagination by re-calling
  with a cursor argument the user wires up manually. An
  `auto_paginate = true` option is conceivable but adds
  complexity; deferred.
- **Regeneration against an OpenAPI spec that has changed.**
  Today, the generator re-reads the spec on every run. If the
  spec drops or renames an operation referenced in HCL, the
  generator fails. That's probably correct — we want explicit
  acknowledgment of the change — but we should consider a
  `--allow-missing-operations` flag for CI scenarios.
- **Do we publish the generated services' HCL specs alongside
  source?** Leaning yes: checking in `mcpgen.hcl` plus generated
  files gives a clear provenance trail. The alternative ("spec
  lives separately in an infra repo") makes regeneration harder
  for service owners.

## References

- ADR-0001 — architecture decisions for mcpgen
- Companion walkthrough Part 1: `docs/guide/building-mcpgen.md`
- Companion walkthrough Part 2: `docs/guide/using-mcpgen.md`
- `docs/guide/mcp-server-in-go.md` — the patterns this generator
  encodes
- HCL2: <https://github.com/hashicorp/hcl>
- `dave/dst`: <https://github.com/dave/dst>
- `pb33f/libopenapi`: <https://github.com/pb33f/libopenapi>
- `text/template`: <https://pkg.go.dev/text/template>
- `go/format`: <https://pkg.go.dev/go/format>
