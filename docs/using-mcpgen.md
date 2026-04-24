# Using mcpgen — Walkthrough, Part 2

A hands-on guide for using mcpgen to generate MCP servers. Covers
each supported mode (new project vs embed), each supported auth
scheme (none, bearer, OIDC, dynamic OIDC), and both backend-spec
paths (inline HCL vs OpenAPI reference). Each section has a
complete HCL example, a description of what the generator produces,
and the deployment notes you need to actually ship the output.

If you're trying to modify mcpgen itself, Part 1
(`building-mcpgen.md`) is the right doc. This one is for service
owners who want to *use* mcpgen.

<!--toc:start-->
<!--toc:end-->

## 1. Install and First Run

```bash
# Install the binary. `@latest` gets the newest; pin for CI.
go install github.com/our-org/mcpgen/cmd/mcpgen@latest

# Sanity check
mcpgen --version
```

From any directory, scaffold a starter config:

```bash
mcpgen init
# writes mcpgen.hcl in the current dir
```

The starter file is deliberately minimal; you edit from there.

## 2. Anatomy of an mcpgen Config

Every HCL file has the same top-level shape:

```hcl
mcpgen_version = "1"

server {
  name    = "<your-service-name>"
  version = "0.1.0"

  listener      { addr = ":7070"; endpoint_path = "/mcp" }
  observability { /* logging, metrics, tracing */ }
  auth          { /* exactly one of: none, bearer, oidc, oidc_dynamic */ }
}

# For proxy mode only:
proxy {
  base_url = "https://api.example.com"
  /* ... */
}

# One tool block per MCP tool:
tool "get_thing" {
  description = "..."
  input       { /* fields */ }
  backend     { /* proxy only */ }
}
```

The rest of this guide is permutations of this shape for each
concrete use case.

## 3. Mode: New Project — Standalone MCP Proxy

You're building a new Go binary that does nothing but proxy an
existing HTTP API through MCP. Pick this when:

- The backend API is stable and has a clear set of operations
  you want to expose as tools.
- You don't want to modify the backend service itself.
- Deployment as a separate Kubernetes Deployment (or equivalent)
  is fine.

### 3.1 Inline HCL Backend — No OpenAPI Required

Use this when the backend API has no OpenAPI spec, or the spec is
out of date, or you only want to expose a subset of operations.

Example: the markdown RFC API. Backend endpoints we want to
expose as tools:

- `GET /rfcs` — list RFCs
- `GET /rfcs/{id}` — fetch one
- `POST /rfcs` — create (write-side; think about auth)

`mcpgen.hcl`:

```hcl
mcpgen_version = "1"

server {
  name    = "rfc-api-mcp"
  version = "1.0.0"

  listener      { addr = ":7070"; endpoint_path = "/mcp" }

  observability {
    logging { format = "json"; level = "info" }
    metrics { enabled = true; path = "/metrics"; addr = ":9090" }
    tracing {
      enabled      = true
      service_name = "rfc-api-mcp"
      sample_ratio = 1.0
      exporter     = "otlp_http"
      endpoint     = "http://otel-collector:4318"
    }
  }

  auth {
    bearer {
      tokens_env = "MCP_TOKENS"
    }
  }
}

proxy {
  base_url = "https://rfc-api.internal"
  auth {
    bearer { token_env = "BACKEND_API_TOKEN" }
  }
  timeouts { dial = "5s"; total = "30s"; idle_connection = "90s" }
  retry    { max_attempts = 3; retry_on_status = [502, 503, 504]; base_delay = "200ms" }
}

tool "list_rfcs" {
  description = "List RFCs, optionally filtered by status."
  input {
    field "status" {
      type        = "string"
      required    = false
      description = "Filter by RFC status. One of: Draft, Proposed, Accepted, Rejected."
    }
    field "limit" {
      type        = "number"
      required    = false
      description = "Max number of RFCs to return. Default 20, max 100."
    }
  }
  backend "http" {
    method = "GET"
    path   = "/rfcs"
    query_param "status" { from = "status"; omit_if_empty = true }
    query_param "limit"  { from = "limit";  omit_if_empty = true }
    response { type = "json" }
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
    path_param "id" { from = "id" }
    response {
      type             = "json"
      content_template = "RFC {{.id}}: {{.title}} ({{.status}})"
    }
    on_error { not_found = "RFC %s not found" }
  }
}

tool "create_rfc" {
  description = "Create a new draft RFC."
  input {
    field "title"    { type = "string"; required = true; description = "RFC title." }
    field "author"   { type = "string"; required = true; description = "Author's handle or email." }
    field "body_md"  { type = "string"; required = true; description = "RFC body in Markdown." }
  }
  backend "http" {
    method = "POST"
    path   = "/rfcs"
    body_json {
      field "title"   { from = "title" }
      field "author"  { from = "author" }
      field "body_md" { from = "body_md" }
    }
    response { type = "json" }
  }
}
```

Generate:

```bash
mcpgen generate --mode new --out ./rfc-api-mcp
cd rfc-api-mcp
go mod tidy
go build ./...
```

You now have a full Go module with:

- `cmd/rfc-api-mcp/main.go` — wires everything up
- `internal/mcpserver/` — server + tool handlers (proxy
  implementations, fully generated)
- `internal/backend/client.go` — typed client for the RFC API
- `internal/mcpauth/` — bearer-token middleware
- `internal/observability/` — slog, metrics, tracing
- `internal/config/` — env-var parsing
- `Dockerfile`, `Makefile`, `go.mod`

**Run it:**

```bash
MCP_TOKENS="alice:abc123,on-call:def456" \
BACKEND_API_TOKEN=$(vault read -field=token /some/path) \
./rfc-api-mcp
```

**Add to Claude Desktop:**

```json
{
  "mcpServers": {
    "rfc-api": {
      "transport": "streamable",
      "url": "https://rfc-api-mcp.internal/mcp",
      "headers": ["Authorization: Bearer abc123"]
    }
  }
}
```

### 3.2 OpenAPI Reference Backend

Use this when the backend API already has an OpenAPI 3.x spec.
mcpgen reads the spec, pulls parameters and response shapes per
operation, and generates the proxy accordingly.

Example: a JSM-compatible API with a real OpenAPI spec at
`./api/jsm-openapi.yaml`.

```hcl
mcpgen_version = "1"

server {
  name    = "jsm-mcp"
  version = "1.0.0"
  listener { addr = ":7070"; endpoint_path = "/mcp" }
  observability { /* same as §3.1 */ }
  auth {
    bearer { tokens_env = "MCP_TOKENS" }
  }
}

proxy {
  base_url = "https://your-tenant.atlassian.net"
  auth {
    # Atlassian APIs use basic auth with email+token
    basic {
      username_env = "JSM_EMAIL"
      password_env = "JSM_API_TOKEN"
    }
  }
  openapi {
    spec = "./api/jsm-openapi.yaml"
  }
}

tool "search_issues" {
  description       = "Search Jira issues using JQL."
  openapi_operation = "searchIssuesUsingJql"
}

tool "get_issue" {
  description       = "Get an issue by its key."
  openapi_operation = "getIssueByKey"
}

tool "create_comment" {
  description       = "Add a comment to an issue."
  openapi_operation = "addIssueComment"
}
```

Three tools, about twenty lines of HCL. The generator:

1. Parses `jsm-openapi.yaml`.
2. For each `openapi_operation`, looks up the operation by
   `operationId`.
3. Pulls the operation's parameters (path, query, body) and builds
   the tool's input schema.
4. Pulls the response schema and uses it as the return type.
5. Generates everything else exactly as in the inline case.

If an `operationId` isn't found in the spec, generation fails with
a pointer to the HCL line that referenced it.

**What the generated tool handler looks like**: the same as the
inline case. The difference is only in how the spec was
*derived*, not in what gets emitted.

### 3.3 When to Mix Inline and OpenAPI

You can mix inline and OpenAPI-referenced tools in a single spec.
Useful for "the API has a spec but one particular tool does
something the spec doesn't quite cover" — for example, a
streaming endpoint that OpenAPI can't describe cleanly but that
you want to expose as a tool.

```hcl
# OpenAPI-driven for the well-behaved operations
tool "search_issues" { openapi_operation = "searchIssuesUsingJql" }
tool "get_issue"     { openapi_operation = "getIssueByKey" }

# Inline for the special case
tool "export_issues" {
  description = "Export search results as CSV."
  input {
    field "jql" { type = "string"; required = true; description = "JQL query." }
  }
  backend "http" {
    method = "GET"
    path   = "/export/csv"
    query_param "jql" { from = "jql" }
    response { type = "text" }  # CSV, treated as a single text blob
  }
}
```

## 4. Mode: Embed — Add MCP to an Existing Service

You have a running service (an HTTP API, typically) and you want
to expose some or all of its capabilities as MCP tools within the
same binary. Pick this when:

- The tools map to in-process business logic that's expensive to
  duplicate (complex authorization rules, DB transactions,
  access to process-local state).
- Deploying a separate MCP binary would add more operational cost
  than the tools justify.
- You want the MCP surface and the HTTP surface to share config
  and observability plumbing.

### 4.1 Prerequisites

Your service's `main.go` needs a marker comment where mcpgen will
insert its registration call:

```go
func main() {
    ctx := context.Background()
    cfg := config.MustLoad()
    logger := observability.NewLogger(cfg)

    mux := http.NewServeMux()
    routes.Register(mux, cfg)

    // mcpgen:hook
    // (mcpgen inserts mcpserver.Register here)

    srv := &http.Server{Addr: cfg.Addr, Handler: mux}
    if err := srv.ListenAndServe(); err != nil {
        logger.Error("server exited", "err", err)
        os.Exit(1)
    }
}
```

If the marker is missing when you run `mcpgen generate --mode
embed`, you'll get a clear error telling you where to put it.
Don't try to work around this — the marker is how mcpgen knows
where to insert safely.

### 4.2 HCL for Embed Mode

The spec looks almost identical to new-project mode, with two
differences:

1. No `proxy {}` block (embed tools call in-process services, not
   HTTP backends).
2. Tool blocks have no `backend {}` block. Instead, mcpgen emits a
   stub that calls a function you'll implement.

Example: adding an MCP tool surface to an existing webhookd service.

```hcl
mcpgen_version = "1"

server {
  name    = "webhookd-mcp"
  version = "1.0.0"
  listener { addr = ":7070"; endpoint_path = "/mcp" }
  observability {
    logging { format = "json"; level = "info" }
    metrics { enabled = true; path = "/metrics" }  # no addr = reuse existing /metrics
    tracing { enabled = true; service_name = "webhookd"; sample_ratio = 1.0
              exporter = "otlp_http"; endpoint = "http://otel-collector:4318" }
  }
  auth {
    bearer { tokens_env = "MCP_TOKENS" }
  }
}

tool "list_recent_deliveries" {
  description = "List recent webhook deliveries."
  input {
    field "limit"    { type = "number"; required = false; description = "Max results, default 20." }
    field "provider" { type = "string"; required = false; description = "Filter by provider." }
  }
}

tool "get_delivery" {
  description = "Fetch delivery detail by request ID."
  input {
    field "request_id" { type = "string"; required = true; description = "Request ID (UUID)." }
  }
}
```

### 4.3 Generate

```bash
mcpgen generate --mode embed --out ./internal/mcp --config ./mcpgen.hcl
```

This writes:

- `internal/mcp/mcpserver/` — server + tool handlers
- `internal/mcp/mcpauth/` — auth middleware
- `internal/mcp/observability/` — only the bits that aren't
  already in your service
- `internal/mcp/mcpserver/service_stubs.go` — stub implementations
  you need to fill in

And edits your `cmd/<svc>/main.go` to insert the registration
call.

### 4.4 Implementing the Stubs

The generated `service_stubs.go` looks like:

```go
// Code generated by mcpgen. EDIT THIS FILE to implement the stubs.
// This file will NOT be regenerated once it exists.
package mcpserver

import "context"

type ListRecentDeliveriesInput struct {
    Limit    *float64 `json:"limit,omitempty"`
    Provider *string  `json:"provider,omitempty"`
}

type ListRecentDeliveriesOutput struct {
    // TODO: define your output shape
}

func (s *Service) ListRecentDeliveries(
    ctx context.Context, in ListRecentDeliveriesInput,
) (ListRecentDeliveriesOutput, error) {
    // TODO: implement
    return ListRecentDeliveriesOutput{}, nil
}

// ... one stub per tool
```

Notice `EDIT THIS FILE` rather than `DO NOT EDIT`. This is the
one generated file mcpgen creates but then hands off to you — it
won't overwrite it on subsequent runs. This is deliberate: the
signatures are derived from your HCL, the bodies are yours.

If you later change a tool's HCL in a way that changes the stub
signature, mcpgen will refuse to regenerate over your edited file
and give you a diff to apply manually. Safer than silently
destroying your implementation.

### 4.5 Sharing Types With Your Existing Code

Your stubs should call into the service code you already have. The
cleanest pattern is to have a thin `Service` type that's
initialized with your existing dependencies:

```go
// internal/mcp/mcpserver/service.go (hand-written, not generated)
package mcpserver

import "webhookd/internal/history"

type Service struct {
    Deliveries *history.Ring
}

// in cmd/webhookd/main.go, where you already build `deliveries`:
mcpService := &mcpserver.Service{Deliveries: deliveries}
// mcpgen:hook
// (after regen:)
// mcpserver.Register(ctx, mux, cfg, mcpService)
```

The generated stub in `service_stubs.go` becomes:

```go
func (s *Service) ListRecentDeliveries(
    ctx context.Context, in ListRecentDeliveriesInput,
) (ListRecentDeliveriesOutput, error) {
    limit := 20
    if in.Limit != nil { limit = int(*in.Limit) }
    entries := s.Deliveries.Recent(limit, in.Provider)
    return ListRecentDeliveriesOutput{Deliveries: entries}, nil
}
```

This is the pattern that makes embed mode worth it: the tool
handler is thin; the work happens in your existing service
package, reused verbatim.

## 5. Auth Schemes

Each scheme is one block in `server.auth`. Only one is allowed per
config. The generated code is structurally similar across schemes
— the `Subject` type and middleware shape are identical — which
means your tool handlers don't care which scheme is in use.

### 5.1 None

```hcl
auth { none {} }
```

No auth middleware. The generator logs a warning at generate
time and inserts a prominent comment in the generated code:

```go
// WARNING: no authentication configured. This server accepts
// unauthenticated requests on the MCP endpoint. Use only for
// local development or behind a strict network policy.
```

**When to use:** local dev, unit tests, dry-run environments
behind an already-authenticated ingress (e.g., a service mesh that
enforces mTLS before traffic ever reaches the pod).

**When not to use:** anything accessible from outside the cluster,
including developer laptops connected via VPN.

### 5.2 Static Bearer

```hcl
auth {
  bearer {
    tokens_env    = "MCP_TOKENS"
    subject_claim = "name"  # optional; defaults to "name"
  }
}
```

Tokens and subjects come from the `MCP_TOKENS` env var as
comma-separated `name:token` pairs:

```bash
MCP_TOKENS="alice:abc123,on-call:def456,automation:xyz789"
```

The middleware:
1. Reads `Authorization: Bearer <token>`.
2. Looks up the token; 401 if missing or unknown.
3. Injects a `Subject{Name: "alice"}` into context.

**Token rotation:** redeploy with a new env var value. Zero
downtime if you overlap old and new tokens during the transition.

**When to use:** internal services, small user populations, when
OIDC is overkill.

**When not to use:** services that need per-user auditing (use
OIDC), services where tokens would outlive the rotation cadence
(use OIDC).

### 5.3 Fixed-Issuer OIDC / JWT

```hcl
auth {
  oidc {
    issuer          = "https://auth.internal/realms/main"
    jwks_url        = "https://auth.internal/realms/main/protocol/openid-connect/certs"
    audience        = "mcp-rfc-api"
    required_scopes = ["mcp:read"]
    subject_claim   = "sub"  # optional; defaults to "sub"
  }
}
```

The generated middleware:
1. Extracts the bearer JWT.
2. Fetches JWKS from `jwks_url` (cached).
3. Validates signature, issuer, audience, expiry.
4. Checks that every value in `required_scopes` is present in the
   token's `scope` claim.
5. Sets `Subject{Name: claims["sub"]}` on context. If
   `subject_claim` is set to something other than `sub`
   (e.g., `email`, `preferred_username`), that claim is used.

**Rotating keys:** your IdP rotates signing keys; the middleware
re-fetches JWKS periodically (default: every hour; currently not
configurable, let us know if you need it).

**When to use:** internal services that already have an IdP
(Keycloak, Azure AD, Okta). Tokens issued to humans or service
accounts.

**When not to use:** cross-organization federation (tokens issued
by many IdPs — use dynamic OIDC).

### 5.4 Dynamic OIDC Discovery

```hcl
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

The difference from fixed OIDC: we don't pre-configure
`jwks_url`. Instead, at startup the middleware fetches
`<issuer>/.well-known/openid-configuration` and discovers the JWKS
URL from it.

```go
provider, err := oidc.NewProvider(ctx, cfg.Issuer)
// provider now knows: jwks_uri, supported algorithms, etc.
verifier := provider.Verifier(&oidc.Config{
    ClientID: cfg.Audience,
})
```

**Why it matters:** if your IdP ever moves JWKS to a different
URL, dynamic OIDC follows; fixed OIDC breaks until you redeploy.

**Startup behavior:** if the discovery endpoint is unreachable at
startup, the server fails to start. This is probably what you
want (better to crash-loop than to serve with broken auth). If
you need "start degraded" behavior, open an issue.

**`cache_ttl`** controls how often JWKS is refreshed. Default
`1h`. Shorter means faster rotation response; longer means fewer
calls to the IdP.

**When to use:** default choice for production. Dynamic discovery
handles IdP changes without code changes.

**When not to use:** air-gapped environments where outbound HTTPS
to the IdP isn't reliable. Use fixed OIDC with JWKS URL cached in
your own service registry.

### 5.5 Auth Scheme Decision Table

| Scheme | Use when | Avoid when |
|---|---|---|
| `none` | Local dev, tests, mesh-protected | Anything reachable from laptops |
| `bearer` | Small team, simple rotation | Need audit, cross-org federation |
| `oidc` | Have an IdP, tolerate manual JWKS config | IdP changes URLs often |
| `oidc_dynamic` | Default for production | Air-gapped / offline envs |

### 5.6 Auth Is the Only Thing That Changes

A key property of the generated code: switching auth schemes
doesn't affect your tool handlers. The `Subject` type and its
extraction from context are the same across schemes. If you start
with `bearer` and later switch to `oidc_dynamic`, the only files
that change are in `internal/mcpauth/`; your tool implementations
don't move.

## 6. Regeneration and CI

### 6.1 Normal Workflow

1. Edit `mcpgen.hcl`.
2. Run `mcpgen generate --mode <new|embed> --out ...`.
3. Run `go build ./...` to check.
4. Commit both the HCL and the generated files.

Regeneration is idempotent: running twice with no HCL changes
produces byte-identical output. This means a CI check of "is
generated code in sync" is one line:

```yaml
- name: Regeneration drift check
  run: |
    mcpgen generate --mode new --out .
    git diff --exit-code
```

If anyone edits generated files directly, this check fails in CI.
Directing them to the HCL is more productive than letting drift
accumulate.

### 6.2 Handling Generator Version Bumps

When mcpgen itself releases a new version, your generated output
may change even with no HCL changes (because templates have been
updated). The recommended workflow:

1. Bump the mcpgen version (`@latest` or pin to the new version).
2. Regenerate.
3. Review the diff — this is exactly what new generator behavior
   looks like.
4. Commit.

For large teams, we recommend pinning mcpgen at a version in a
dedicated `tools.go` file:

```go
//go:build tools
// +build tools

package tools

import _ "github.com/our-org/mcpgen/cmd/mcpgen"
```

And a `Makefile` target:

```make
.PHONY: gen
gen:
	go install github.com/our-org/mcpgen/cmd/mcpgen
	mcpgen generate --mode new --out .
```

Bumping is then a single `go get -u github.com/our-org/mcpgen &&
make gen` instead of a conversation about which version everyone
has installed.

### 6.3 Testing the Generated Server

You don't have to test the observability spine — mcpgen tests its
own templates. What you do have to test:

- **For embed mode:** the stub implementations you filled in.
  These are plain Go functions; write table-driven unit tests for
  them in your existing test directory.
- **For proxy mode:** integration tests against the backend API
  using `httptest.NewServer` as a stand-in backend. The generated
  backend client is typed, so the tests are straightforward.
- **Tool discovery and schema shape:** run MCP Inspector against
  the server once, confirm the tool catalog looks right. Bake this
  into a smoke test if you want to automate.

## 7. Common Patterns and Pitfalls

### 7.1 Describing Tool Inputs Well

The `description` on every tool and every input field is the
prompt the LLM reads to decide how to use the tool. Good
descriptions cost nothing at runtime and dramatically improve
tool-use reliability.

Bad:
```hcl
tool "get_rfc" {
  description = "Gets an RFC."
  input {
    field "id" { type = "string"; required = true; description = "The id." }
  }
}
```

Good:
```hcl
tool "get_rfc" {
  description = "Fetch an RFC by its identifier. Returns the RFC's title, status, author, and body in Markdown."
  input {
    field "id" {
      type        = "string"
      required    = true
      description = "RFC identifier in the form 'RFC-NNNN', e.g. 'RFC-0042'. Case-insensitive."
    }
  }
}
```

The second version tells the LLM the exact format, that case
doesn't matter, and what it can expect back. Those are the
sentences that make tool use work.

### 7.2 Write-Side Tools

Tools that modify state need more care than read tools:

- Consider adding a `dry_run` input that makes the tool compute
  its effect without committing.
- Use OIDC with required scopes to gate access.
- Log subject, action, and target in every invocation (mcpgen does
  this automatically, but make sure your retention is set
  correctly for audit).

### 7.3 OpenAPI Gotchas

- **OperationId is case-sensitive.** `getRfc` and `GetRFC` are
  different.
- **Referenced schemas resolve recursively.** If your response
  schema has `$ref`s to other schemas, mcpgen resolves them. If
  any `$ref` is broken or external-URL, generation fails.
- **Authentication schemes in the OpenAPI spec are ignored.**
  mcpgen uses the `proxy.auth` block for how to authenticate to
  the backend, not whatever the spec declares. This is
  deliberate: the spec might describe "any auth" while your
  deployment requires a specific token.

### 7.4 Embed Mode: Don't Forget the Marker

The `// mcpgen:hook` marker in your `main.go` has to exist before
the first `mcpgen generate --mode embed`. If you forget, the
error message tells you what to do:

```
Error: marker comment // mcpgen:hook not found in main()

Add the following comment to your main.go at the point where you
want the MCP server to be registered:

    // mcpgen:hook

Then re-run mcpgen.
```

### 7.5 What to Do When Something Goes Wrong

- **`mcpgen generate` fails with a diagnostic pointing to an HCL
  line:** fix the HCL. The diagnostic is right.
- **`mcpgen generate` succeeds but `go build` fails in the
  generated code:** this is an mcpgen bug. File an issue with the
  HCL file attached and the `go build` error. Workaround: pin to
  the previous mcpgen version.
- **Generated code passes `go build` but the runtime server
  doesn't work:** usually a config problem (missing env var, wrong
  endpoint URL). Check the logs first; mcpgen generates
  informative slog lines.
- **Claude Desktop can't connect:** check the `Host` header — the
  generated server validates against `WEBHOOK_MCP_ALLOWED_HOSTS`
  (or equivalent). If your client connects via a different
  hostname than what's allowed, you get a 400.

## 8. Summary Table — What Goes Where

| Need | Mode | Backend | Auth |
|---|---|---|---|
| Expose RFC API as MCP, separate service | `new` | `backend.http` (inline) | `bearer` (small team) or `oidc` |
| Proxy Jira via its OpenAPI spec | `new` | `openapi_operation` | `oidc_dynamic` |
| Add MCP tools to webhookd for on-call | `embed` | stubs | `bearer` |
| Add MCP tools for end-user self-service | `embed` | stubs | `oidc_dynamic` with scopes |
| Local dev proxy, no auth | `new` | inline | `none` |

## 9. What's Next

If you need features mcpgen doesn't support yet, check the "Open
Questions" section of `DESIGN-0004` — many of the things people
ask for are already on the v1.1 list. File an issue with your
concrete use case; we'd rather expand the spec than have you
hand-roll around the generator.

## References

- ADR-0001 — mcpgen architecture decisions
- DESIGN-0004 — mcpgen detailed design
- `docs/guide/building-mcpgen.md` — Part 1: how the generator works
- `docs/guide/mcp-server-in-go.md` — the underlying Go MCP patterns
- HCL2 reference: <https://github.com/hashicorp/hcl>
- MCP specification: <https://modelcontextprotocol.io/specification>
- MCP Inspector (for testing generated servers):
  <https://github.com/modelcontextprotocol/inspector>
