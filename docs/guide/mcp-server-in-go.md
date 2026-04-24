# Building an MCP Server in Go — A Walkthrough

A reusable guide for adding a Model Context Protocol (MCP) server to a Go
service. Written so it applies equally to webhookd, the markdown RFC API, or any
other Go backend. The worked examples are deliberately neutral — substitute your
own tool names and business logic.

<!--toc:start-->
<!--toc:end-->

## 1. What You're Building

MCP is a JSON-RPC 2.0 protocol that lets an LLM client (Claude Desktop, Claude
Code, etc.) discover and invoke capabilities exposed by a server. The server
advertises three kinds of things:

- **Tools** — functions the client can call. This is the primary surface for
  "make the LLM able to do X."
- **Resources** — read-only data surfaces. The client decides when to fetch.
  Useful for exposing documents, configs, or generated content.
- **Prompts** — parameterized prompt templates the client can instantiate.

For most services, **tools are all you need.** Resources and prompts are
available if they fit your use case, but this guide focuses on tools because
they're what 95% of useful MCP servers ship.

## 2. Decision Tree: Do You Need This?

Before you write code, check the decision tree:

- **Does the service expose data or actions that a human currently does through
  a CLI or dashboard?** If yes, an MCP server probably helps. If the service is
  purely a backend for other services (no human operators), probably not.
- **Do you want the LLM to _do_ things (write side) or just _read_ things (read
  side)?** Both are fine. Write-side tools need more care — authorization,
  audit, dry-run modes.
- **Is your service long-lived (a server) or short-lived (a CLI)?** Servers get
  Streamable HTTP. CLIs get stdio. This guide focuses on the server case.

If you answered "yes, server, writes-and-reads," proceed.

## 3. SDK Choice

Two options in the Go ecosystem:

| SDK      | Module path                              | Status                                                                                  |
| -------- | ---------------------------------------- | --------------------------------------------------------------------------------------- |
| mcp-go   | `github.com/mark3labs/mcp-go`            | Community standard, battle-tested, implements MCP spec 2025-11-25 with backward compat. |
| Official | `github.com/modelcontextprotocol/go-sdk` | Official but newer; adoption is climbing.                                               |

**Recommended for now: `mcp-go`.** Wider deployment, better documented, most
example code online uses it. Migration to the official SDK is mechanical if we
move later — both wrap the same JSON-RPC wire format and the handler signatures
are similar.

Install:

```bash
go get github.com/mark3labs/mcp-go
```

## 4. Transport: Streamable HTTP

MCP defines two official transports:

- **stdio** — the server process reads JSON-RPC from stdin, writes to stdout.
  Used when the client spawns the server as a subprocess. Simplest for local
  tools.
- **Streamable HTTP** — the server listens on an HTTP port at a single endpoint
  (conventionally `/mcp`), handling POST for requests and optionally streaming
  responses over SSE. Used when the server is long-lived and clients connect
  remotely.

**For services, always Streamable HTTP.** stdio only makes sense when the server
is ephemeral and co-located with the client.

Older MCP servers used a deprecated SSE transport with two endpoints (`/sse` and
a separate POST endpoint). **Don't use it for new servers.** The spec deprecated
it in favor of Streamable HTTP in March 2025. `mcp-go` still supports it for
backward compat; pick Streamable HTTP.

## 5. Minimum Viable Server

The shortest meaningful server — a single tool, Streamable HTTP, no auth, no
observability:

```go
package main

import (
    "context"
    "log"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    s := server.NewMCPServer(
        "example-server", "1.0.0",
        server.WithToolCapabilities(true),
        server.WithRecovery(),
    )

    echo := mcp.NewTool("echo",
        mcp.WithDescription("Return the input verbatim."),
        mcp.WithString("message",
            mcp.Required(),
            mcp.Description("Text to echo back."),
        ),
    )
    s.AddTool(echo, handleEcho)

    httpServer := server.NewStreamableHTTPServer(s)
    if err := httpServer.Start(":7070"); err != nil {
        log.Fatal(err)
    }
}

func handleEcho(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    msg, err := req.RequireString("message")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }
    return mcp.NewToolResultText(msg), nil
}
```

The client connects to `http://localhost:7070/mcp` (the default path; override
with `server.WithEndpointPath`).

A few things to notice even in this tiny example:

- **`server.WithRecovery()`** catches panics in tool handlers. Always include
  it.
- **`mcp.NewToolResultError(err.Error())`** returns an error to the client as a
  normal tool result rather than a JSON-RPC error. This is the right pattern for
  user-visible errors — the LLM can see the message and potentially recover.
- Returning a Go `error` from the handler is reserved for protocol-level
  failures (context cancellation, transport errors). User errors go through
  `NewToolResultError`.

## 6. Tool Design

A tool is a typed function the LLM can call. Good tools have four properties:

1. **Narrow scope.** One tool, one verb. `create_user` yes; `manage_user` (that
   takes an action string) no — the LLM has to guess which actions exist, and
   tool-call argument validation can't help it.
2. **Self-describing.** Description and parameter descriptions are not optional.
   They are the prompt the LLM reads to decide whether to call the tool. Write
   them as if explaining the tool to a new engineer with no context.
3. **Validated input.** Use the schema — `mcp.Required()`, `mcp.Enum(...)`,
   `mcp.Min`, `mcp.Max`, `mcp.Pattern` — to fail bad calls at the protocol
   boundary rather than in your handler.
4. **Structured output where possible.** `NewToolResultStructured` returns JSON
   the client can parse; `NewToolResultText` returns a string the LLM reads as
   text. Prefer structured for tools whose output is operated on by the LLM or
   other tools; prefer text for tools whose output is displayed to a human.

### 6.1 Tool Naming

- `snake_case` throughout, per MCP convention.
- Start with a verb: `list_*`, `get_*`, `create_*`, `delete_*`, `search_*`.
  Makes the tool catalog skimmable.
- Scope the noun to your domain: `list_recent_deliveries`, not `list_recent`.
- Avoid one-word names (`status`, `check`) — they collide with other servers'
  tools.

### 6.2 Input Schemas

Use the builder API for common cases:

```go
tool := mcp.NewTool("search_issues",
    mcp.WithDescription("Search JSM issues by JQL."),
    mcp.WithString("jql",
        mcp.Required(),
        mcp.Description("A JQL query string. Example: project = SEC AND status = Open"),
    ),
    mcp.WithNumber("limit",
        mcp.Description("Max number of issues to return."),
        mcp.Min(1), mcp.Max(100),
    ),
    mcp.WithString("order_by",
        mcp.Description("Field to sort by."),
        mcp.Enum("created", "updated", "priority"),
    ),
)
```

For complex nested inputs, you can provide a raw JSON schema via
`mcp.WithInputSchema`. Keep tool inputs flat when possible — LLMs construct
nested arguments less reliably than flat ones.

### 6.3 Output Shapes

**Text output** for human-readable results:

```go
return mcp.NewToolResultText(fmt.Sprintf(
    "Found %d issues matching the query.", len(issues))), nil
```

**Structured output** for data the LLM will operate on:

```go
return mcp.NewToolResultStructured(map[string]any{
    "total":   len(issues),
    "issues":  issues,
}), nil
```

**Error result** for user-visible errors:

```go
return mcp.NewToolResultError(
    fmt.Sprintf("unknown project %q", project)), nil
```

## 7. Mounting MCP in an Existing HTTP Service

If your service already runs an HTTP server (webhookd does, the markdown RFC API
does), you have two options:

### 7.1 Option A: Separate Listener (Recommended for Services)

Give MCP its own `*http.Server` on its own port. Rationale:

- Different auth model. MCP uses bearer tokens; your main API may use mTLS,
  HMAC, or OAuth.
- Different traffic profile. MCP is operator traffic, typically low volume. Your
  main API is the hot path.
- Different failure domain. An LLM making a bad tool call should never degrade
  the main API.

```go
// Main API on :8080
apiSrv := &http.Server{Addr: ":8080", Handler: apiHandler}

// MCP on :7070
mcpServer := server.NewStreamableHTTPServer(mcp)
mcpHandler := auth.Middleware(mcpServer) // your middleware
mcpSrv := &http.Server{Addr: ":7070", Handler: mcpHandler}

go apiSrv.ListenAndServe()
go mcpSrv.ListenAndServe()
```

### 7.2 Option B: Same Listener, Different Path

Mount MCP at `/mcp` on the same mux as your API. Works if the auth model is
already uniform (single bearer token good for both, say). Watch out for
[mcp-go issue #493](https://github.com/mark3labs/mcp-go/issues/493) — the
`StreamableHTTPServer` can mishandle GET requests to sibling paths on the same
mux. Until fixed, separate listeners are safer.

```go
mux := http.NewServeMux()
mux.Handle("POST /api/...", apiHandler)
mux.Handle("/mcp", mcpServer) // handles both POST and GET
http.ListenAndServe(":8080", mux)
```

## 8. Authentication

MCP doesn't mandate an auth scheme — it's a transport concern. For HTTP, three
options:

### 8.1 Bearer Token (Simple, Good Default)

Static tokens configured out-of-band, validated in middleware:

```go
func BearerAuth(tokens map[string]string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            h := r.Header.Get("Authorization")
            if !strings.HasPrefix(h, "Bearer ") {
                http.Error(w, "missing bearer", http.StatusUnauthorized)
                return
            }
            subj, ok := tokens[strings.TrimPrefix(h, "Bearer ")]
            if !ok {
                http.Error(w, "invalid token", http.StatusUnauthorized)
                return
            }
            ctx := context.WithValue(r.Context(), subjectKey{}, subj)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

Tokens in config via env var (`MCP_TOKENS=alice:abc123,bob:def456`), rotated by
redeploy. Good for small teams and internal servers.

### 8.2 OIDC / JWT

For SSO-integrated services, validate JWTs from your IdP. Use
`github.com/coreos/go-oidc/v3/oidc` for the verifier. Middleware shape is the
same as bearer, with the token being a JWT the middleware validates.

### 8.3 OAuth 2.1 Dynamic Client Registration

The MCP spec direction for multi-tenant remote servers. Complex; only worth it
if you're serving users across organizations with no pre-provisioned
credentials. For internal/team servers, skip.

### 8.4 Host Header Validation (Always)

Regardless of auth scheme, validate the `Host` header against an allow-list.
**DNS rebinding attacks against localhost MCP servers have been demonstrated in
the wild** — a malicious webpage can convince the browser to send authenticated
requests to your local-only MCP server if `Host` is unchecked.

```go
func HostAllowlist(allowed []string) func(http.Handler) http.Handler {
    set := make(map[string]struct{}, len(allowed))
    for _, h := range allowed { set[h] = struct{}{} }
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if _, ok := set[r.Host]; !ok {
                http.Error(w, "host not allowed", http.StatusBadRequest)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## 9. Context, Subject, and Request Headers in Handlers

Tool handlers receive a `context.Context`. Everything you need to know about the
caller comes from context.

### 9.1 Reading the Authenticated Subject

The auth middleware puts the subject into context:

```go
func handleThing(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    subj, ok := SubjectFromContext(ctx)
    if !ok {
        return mcp.NewToolResultError("unauthenticated"), nil
    }
    // use subj.Name for logging, authorization, etc.
}
```

### 9.2 Reading Request Headers

mcp-go exposes HTTP headers directly on the `CallToolRequest`:

```go
func handleThing(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    requestID := req.Header.Get("X-Request-ID")
    traceParent := req.Header.Get("traceparent")
    // ...
}
```

This is how you pick up client-provided request IDs or trace context. If the
client propagates OTel headers, you get tracing continuity across the MCP
boundary for free.

### 9.3 Propagating to Downstream Calls

Pass the handler's `ctx` down to every downstream call: HTTP clients, DB
queries, K8s clients. This carries cancellation, deadlines, and OTel span
context all in one value — it's the same discipline you'd apply in any Go HTTP
handler.

## 10. Observability

Three signals, one pattern: treat every tool invocation like an HTTP request for
instrumentation purposes.

### 10.1 Spans

Start a span at the top of each handler:

```go
ctx, span := tracer.Start(ctx, "mcp.tool.get_issue",
    trace.WithAttributes(
        attribute.String("mcp.subject", subj.Name),
        attribute.String("mcp.tool", "get_issue"),
    ))
defer span.End()
```

Downstream spans (HTTP calls, DB queries) nest under this automatically because
they receive the same `ctx`. The result is that a single tool call produces a
complete trace of every side effect it caused — this is the tracing story MCP is
good at.

If the caller propagates W3C `traceparent` via the `CallToolRequest.Header`,
configure an OTel HTTP server instrumentation on the Streamable HTTP listener so
the root span is a continuation rather than a new root. Same pattern as the
Phase 1 receiver (`otelhttp.NewHandler` wrapping the mux).

### 10.2 Metrics

Standard Prometheus approach:

```go
mcpToolInvocations := prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "svc_mcp_tool_invocations_total",
        Help: "MCP tool invocations, labeled by tool, subject, and outcome.",
    },
    []string{"tool", "subject", "outcome"},
)

mcpToolDuration := prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "svc_mcp_tool_duration_seconds",
        Help:    "Tool invocation latency.",
        Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
    },
    []string{"tool", "outcome"},
)
```

**Cardinality watch:** `subject` is a label. If subjects are bounded (a known
set of operators), fine. If subjects can be any authenticated user in a large
org, drop the `subject` label from the main metric and use a separate
low-cardinality rollup (`invocations_by_subject{subject}` with a shorter
retention).

### 10.3 Structured Logs

One info line per tool invocation, with `trace_id` / `span_id` already
correlated by your slog handler (see webhookd Phase 1 §4.1 for the
`traceHandler` pattern — works identically here):

```go
slog.InfoContext(ctx, "mcp tool completed",
    "tool", "get_issue",
    "mcp.subject", subj.Name,
    "outcome", "success",
    "duration_ms", time.Since(start).Milliseconds(),
)
```

## 11. The Handler Template

Combining the above, every tool handler ends up with the same skeleton:

```go
func (t *Tools) GetIssue(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    start := time.Now()

    // 1. Subject (from auth middleware).
    subj, _ := SubjectFromContext(ctx)

    // 2. Span.
    ctx, span := t.tracer.Start(ctx, "mcp.tool.get_issue",
        trace.WithAttributes(
            attribute.String("mcp.subject", subj.Name),
        ))
    defer span.End()

    // 3. Input parsing + validation.
    key, err := req.RequireString("issue_key")
    if err != nil {
        span.RecordError(err)
        t.recordOutcome("get_issue", subj.Name, "bad_input", start)
        return mcp.NewToolResultError(err.Error()), nil
    }

    // 4. Authorization (if tool is sensitive).
    if !t.policy.Allow(subj, "read", "issue", key) {
        t.recordOutcome("get_issue", subj.Name, "forbidden", start)
        return mcp.NewToolResultError("not allowed"), nil
    }

    // 5. Actual work.
    issue, err := t.store.GetIssue(ctx, key)
    if err != nil {
        span.RecordError(err)
        t.recordOutcome("get_issue", subj.Name, "error", start)
        slog.ErrorContext(ctx, "get_issue failed", "err", err)
        return mcp.NewToolResultError(err.Error()), nil
    }

    // 6. Result.
    t.recordOutcome("get_issue", subj.Name, "success", start)
    slog.InfoContext(ctx, "mcp tool completed",
        "tool", "get_issue", "mcp.subject", subj.Name,
        "outcome", "success",
        "duration_ms", time.Since(start).Milliseconds())
    return mcp.NewToolResultStructured(issue), nil
}
```

If three tools in, you notice steps 1-2 and 6 repeating, extract them into a
helper. Don't over-abstract early — the template is small enough to keep
visible.

## 12. Testing

### 12.1 Unit Tests

Tool handlers are plain Go functions. Test them with fabricated
`mcp.CallToolRequest` values:

```go
func TestGetIssue(t *testing.T) {
    tools := newTestTools(t)

    req := mcp.CallToolRequest{}
    req.Params.Name = "get_issue"
    req.Params.Arguments = map[string]any{"issue_key": "SEC-1234"}

    ctx := context.WithValue(context.Background(),
        subjectKey{}, Subject{Name: "alice"})

    res, err := tools.GetIssue(ctx, req)
    if err != nil {
        t.Fatalf("GetIssue: %v", err)
    }
    if res.IsError {
        t.Fatalf("GetIssue returned error: %v", res.Content)
    }
}
```

### 12.2 Integration Tests

Start the actual MCP server bound to a random port, connect with the SDK's
client:

```go
func TestMCPServer_Integration(t *testing.T) {
    mcpServer := buildTestServer(t)
    httpServer := server.NewTestStreamableHTTPServer(mcpServer)
    defer httpServer.Close()

    cl, err := client.NewStreamableHttpClient(httpServer.URL + "/mcp")
    // ... initialize, call tools, assert results
}
```

`server.NewTestStreamableHTTPServer` is a helper in mcp-go that wires up an
`httptest.Server` with Streamable HTTP routing.

### 12.3 Interactive Testing — MCP Inspector

The MCP Inspector is the canonical manual-testing tool:

```bash
npx @modelcontextprotocol/inspector \
  --transport streamable-http \
  http://localhost:7070/mcp \
  --header "Authorization: Bearer <token>"
```

Opens a browser UI where you can list tools, inspect their schemas, and invoke
them with arbitrary arguments. Use this during development — it's faster than
wiring up Claude Desktop.

## 13. Deploying to Claude Desktop / Claude Code

Once the server is up and reachable, users configure their MCP client to
connect. For Claude Desktop, edit
`~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or
`%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "our-service": {
      "transport": "streamable",
      "url": "https://mcp.our-service.internal/mcp",
      "headers": ["Authorization: Bearer abc123"]
    }
  }
}
```

For Claude Code, the equivalent configuration lives in
`~/.config/claude-code/mcp.json` (or whatever your version uses). Restart the
client; the tools appear in the catalog.

For clients that only support stdio, use the `mcp-remote` companion
(`npx mcp-remote <url>`) as a stdio-to-HTTP bridge.

## 14. Patterns Worth Knowing

**Dry-run flags on write tools.** For every `create_*` or `delete_*` tool,
consider adding `dry_run: true`. The LLM often calls tools exploratorily; a
dry-run first lets it preview the impact before committing.

**Pagination on list tools.** Return at most N items per call with a
`next_cursor` field. LLMs handle pagination cleanly when the token is explicit.

**Consistent error shapes.** If a tool returns
`{"error": "not found", "details": {...}}` via `NewToolResultStructured`, the
LLM can operate on it. A plain text "not found" via `NewToolResultError` is fine
but less useful for chained tool use.

**Avoid tools that encode large amounts of free-form user intent.** "Do whatever
the user wants with this string" is not a tool, it's an orchestration hole.
Break it into specific tools the LLM can compose.

**Cache tool definitions locally when possible.** Tool discovery happens on
every client connection; a handful of tools is fine, a hundred is not. Group
related capabilities into one tool with an `action` parameter — if and only if
the actions are genuinely substitutable in input and output shape.

## 15. Things People Get Wrong

- **Returning errors from `tool(ctx)` for user-visible errors.** Use
  `NewToolResultError`. Returning `error` is reserved for transport-level
  failures.
- **Forgetting `server.WithRecovery()`.** A panicking handler takes down the
  whole session.
- **Not validating `Host` header.** DNS rebinding is a real attack class. The
  `Host` allow-list middleware takes ten lines and prevents the entire class.
- **Logging secrets as tool outputs.** Tool outputs are sent to the client and
  logged by every MCP-aware proxy in between. Mask sensitive values.
- **Tool descriptions that say "see docs" or are single words.** The description
  is the prompt the LLM reads to pick the tool. Write it as documentation, not
  as a code comment.
- **Global state in tool handlers.** Handlers run concurrently; share state via
  explicit types with appropriate locking, same as any Go HTTP handler.

## 16. Reference: The Smallest Useful Production Server

```go
package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "time"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

type tokensType map[string]string

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    slog.SetDefault(logger)

    tokens := parseTokens(os.Getenv("MCP_TOKENS"))
    allowedHosts := []string{"localhost", "localhost:7070", "127.0.0.1:7070"}

    s := server.NewMCPServer(
        "our-service-mcp", "1.0.0",
        server.WithToolCapabilities(true),
        server.WithRecovery(),
    )
    registerTools(s)

    mcpHandler := server.NewStreamableHTTPServer(s)
    handler := hostAllowlist(allowedHosts)(
        bearerAuth(tokens)(mcpHandler))

    srv := &http.Server{
        Addr:              ":7070",
        Handler:           handler,
        ReadHeaderTimeout: 5 * time.Second,
    }
    slog.Info("mcp listening", "addr", ":7070")
    if err := srv.ListenAndServe(); err != nil {
        slog.Error("mcp server exited", "err", err)
        os.Exit(1)
    }
}

// bearerAuth, hostAllowlist, parseTokens, registerTools, and tool
// handlers follow the patterns in §8-§11 above.
func bearerAuth(tokens tokensType) func(http.Handler) http.Handler { /* ... */ return nil }
func hostAllowlist(allowed []string) func(http.Handler) http.Handler { /* ... */ return nil }
func parseTokens(s string) tokensType { /* ... */ return nil }
func registerTools(s *server.MCPServer) { _ = s; /* AddTool calls */ }

var _ = context.TODO // placeholder to keep imports
var _ mcp.CallToolRequest
```

Two hundred lines total, including all the middleware and a couple of tool
handlers. That's the thing to aim at.

## References

- MCP specification: <https://modelcontextprotocol.io/specification>
- Streamable HTTP transport:
  <https://modelcontextprotocol.io/specification/2025-03-26/basic/transports>
- `mark3labs/mcp-go`: <https://github.com/mark3labs/mcp-go>
- MCP Inspector: <https://github.com/modelcontextprotocol/inspector>
- Official Go SDK (alternative):
  <https://github.com/modelcontextprotocol/go-sdk>
