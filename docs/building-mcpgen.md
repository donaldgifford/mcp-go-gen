# Building mcpgen — Walkthrough, Part 1

A deep walkthrough of *how* mcpgen is built: the pipeline from HCL
input to Go source output, how `text/template` produces most of the
code, how `dave/dst` handles the parts templates can't do well, and
the tricks that make the generator idempotent and safe to re-run.

This is Part 1 of two. Part 2 (`using-mcpgen.md`) is the user-facing
guide for actually running the tool against real services.

If you've never built a Go code generator before, the most useful
sections for you are §3 (the pipeline), §5 (templates in practice),
and §7 (DST for edits to existing code). The rest fills in the
corners.

<!--toc:start-->
<!--toc:end-->

## 1. What We're Building

mcpgen is a command-line tool: you give it an HCL file describing
an MCP server, it writes Go files that implement that server. Two
invocations:

```bash
# Scaffold a new standalone MCP service
mcpgen generate --mode new --out ./rfc-api-mcp

# Add MCP to an existing Go service
mcpgen generate --mode embed --out ./internal/mcp
```

Under the hood, mcpgen has to:

1. Parse and validate the HCL config.
2. Optionally parse an OpenAPI spec and merge operation metadata
   into the config.
3. Build an intermediate representation (IR) that's independent of
   both HCL and OpenAPI syntax.
4. Render Go source files by running templates over the IR.
5. For embed mode, also make surgical edits to an existing
   `main.go` using DST.
6. Run everything through `go/format.Source()` before writing.
7. For new-project mode, additionally scaffold a module skeleton
   (go.mod, Dockerfile, Makefile).

The rest of this walkthrough explains each step. Code samples are
simplified to the essence — the real generator has more error
handling and test hooks, but the shape is the same.

## 2. Module Layout

```
mcpgen/
├── cmd/mcpgen/
│   └── main.go                 # CLI entry, flag parsing, dispatch
├── internal/
│   ├── cli/
│   │   ├── init.go             # `mcpgen init`
│   │   ├── validate.go         # `mcpgen validate`
│   │   └── generate.go         # `mcpgen generate`
│   ├── config/
│   │   ├── schema.go           # HCL struct types (the "what can be written")
│   │   └── load.go             # parse + diagnostic handling
│   ├── openapi/
│   │   └── resolve.go          # libopenapi -> operation maps
│   ├── ir/
│   │   ├── ir.go               # IR types
│   │   └── build.go            # config + openapi -> IR
│   ├── gen/
│   │   ├── templates/          # //go:embed source of all templates
│   │   │   ├── main.go.tmpl
│   │   │   ├── server.go.tmpl
│   │   │   ├── tools.go.tmpl
│   │   │   ├── auth_bearer.go.tmpl
│   │   │   ├── auth_oidc.go.tmpl
│   │   │   └── ...
│   │   ├── render.go           # template funcs + render loop
│   │   └── write.go            # format + write-to-disk
│   ├── embed/
│   │   └── hook.go             # DST edit of target main.go
│   └── scaffold/
│       └── newproject.go       # go.mod, Dockerfile, Makefile
└── testdata/
    ├── fixtures/*.hcl          # HCL inputs
    └── golden/*.go             # expected outputs
```

Three rules govern this layout:

1. **The IR is the pinch point.** Everything upstream of it
   (config, openapi) only knows how to build it. Everything
   downstream (templates, dst) only knows how to consume it.
   Changing either end doesn't ripple through the whole tool.
2. **Templates live in one directory, embedded into the binary.**
   No runtime template loading from disk; the binary is
   self-contained.
3. **Everything in `internal/`.** mcpgen is a tool, not a
   library. Nothing outside `cmd/mcpgen` should ever depend on
   these packages.

## 3. The Pipeline

Textually:

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   mcpgen.hcl │     │ openapi.yaml │     │ target main.go│
│   (required) │     │   (optional) │     │  (embed only)│
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                     │
       │ gohcl.DecodeBody   │ libopenapi          │
       ▼                    ▼                     │
┌──────────────┐     ┌──────────────┐             │
│ config.Config│     │ openapi ops  │             │
└──────┬───────┘     └──────┬───────┘             │
       │                    │                     │
       └─────────┬──────────┘                     │
                 │                                │
                 │  ir.Build(cfg, ops)            │
                 ▼                                │
          ┌──────────────┐                        │
          │   ir.Spec    │                        │
          └──────┬───────┘                        │
                 │                                │
     ┌───────────┼───────────┐                    │
     │           │           │                    │
     ▼           ▼           ▼                    ▼
 templates   templates   templates        DST edit of main.go
     │           │           │                    │
     └───────────┴───────────┴────────────────────┤
                                                  │
                              ┌───────────────────┘
                              ▼
                      go/format.Source()
                              │
                              ▼
                     atomic file write to disk
```

The two cardinal rules of this pipeline:

- **Templates and DST are terminal stages.** They only *emit*; they
  never *read* anything from disk that isn't an input file. The
  generator's state at the end of each stage is a pure function
  of inputs plus IR.
- **Disk writes are the last thing that happens.** If any stage
  fails, no partial output is left behind. The renderers build a
  list of `(path, bytes)` pairs; only when every render succeeded
  do we actually write.

## 4. HCL Decoding

### 4.1 Schema Types

Schema lives in `internal/config/schema.go`. These are plain Go
structs with `hcl` tags; they define exactly what can appear in an
mcpgen HCL file.

```go
// internal/config/schema.go
package config

type Config struct {
    Version string `hcl:"mcpgen_version"`
    Server  Server `hcl:"server,block"`
    Proxy   *Proxy `hcl:"proxy,block"`
    Tools   []Tool `hcl:"tool,block"`
}

type Server struct {
    Name     string         `hcl:"name"`
    Version  string         `hcl:"version"`
    Listener Listener       `hcl:"listener,block"`
    Obs      *Observability `hcl:"observability,block"`
    Auth     Auth           `hcl:"auth,block"`
}

type Auth struct {
    None        *AuthNone        `hcl:"none,block"`
    Bearer      *AuthBearer      `hcl:"bearer,block"`
    OIDC        *AuthOIDC        `hcl:"oidc,block"`
    OIDCDynamic *AuthOIDCDynamic `hcl:"oidc_dynamic,block"`
}

type AuthBearer struct {
    TokensEnv    string `hcl:"tokens_env"`
    SubjectClaim string `hcl:"subject_claim,optional"`
}

type Tool struct {
    Name             string       `hcl:"name,label"`
    Description      string       `hcl:"description"`
    OpenAPIOperation string       `hcl:"openapi_operation,optional"`
    Input            *ToolInput   `hcl:"input,block"`
    Backend          *ToolBackend `hcl:"backend,block"`
}
```

Key tag conventions:

- **`hcl:"name"`** — attribute. The HCL looks like `name = "..."`.
- **`hcl:"name,block"`** — nested block. HCL looks like `name { ... }`.
- **`hcl:"name,label"`** — the block's label (the `"get_rfc"` part
  of `tool "get_rfc" { ... }`).
- **`hcl:"name,optional"`** — like `attr` but the field may be
  omitted.

The four concrete auth types appear as optional block fields on
`Auth` so HCL can write `auth { bearer { ... } }`. Validation that
exactly one is set happens after decode.

### 4.2 Parsing

`hclsimple.DecodeFile` is the one-shot for simple schemas:

```go
// internal/config/load.go
package config

import (
    "fmt"
    "github.com/hashicorp/hcl/v2/hclsimple"
)

func Load(path string) (*Config, error) {
    var cfg Config
    if err := hclsimple.DecodeFile(path, nil, &cfg); err != nil {
        return nil, fmt.Errorf("load %s: %w", path, err)
    }
    return &cfg, nil
}
```

For richer error reporting — which mcpgen wants because HCL
diagnostics are half the reason to use HCL — we drop to the
underlying parser:

```go
import (
    "github.com/hashicorp/hcl/v2"
    "github.com/hashicorp/hcl/v2/hclparse"
    "github.com/hashicorp/hcl/v2/gohcl"
)

func LoadDiag(path string) (*Config, hcl.Diagnostics) {
    parser := hclparse.NewParser()
    file, diags := parser.ParseHCLFile(path)
    if diags.HasErrors() {
        return nil, diags
    }

    var cfg Config
    moreDiags := gohcl.DecodeBody(file.Body, nil, &cfg)
    diags = append(diags, moreDiags...)
    if diags.HasErrors() {
        return nil, diags
    }
    return &cfg, nil
}
```

Diagnostics are pointed to specific source ranges, so the CLI can
render them as:

```
Error: unsupported block

  on mcpgen.hcl line 12:
  12: auth {
  13:   magic { ... }
          ^^^^^

Blocks of type "magic" are not expected here.
```

using HCL's `hcl.NewDiagnosticTextWriter`. The quality of these
messages is what justifies using HCL over YAML — error pointers
cost one extra function call.

### 4.3 Post-decode Validation

After decoding, `config.Validate(&cfg)` enforces rules the schema
tags can't express:

- Exactly one of `auth.none` / `auth.bearer` / `auth.oidc` /
  `auth.oidc_dynamic` is set.
- Tool names are unique and valid MCP tool identifiers
  (`^[a-z][a-z0-9_]*$`).
- If any tool sets `openapi_operation`, `proxy.openapi.spec` is
  present.
- Listener `addr` parses as a valid `net.Listen` address.
- If `mcpgen_version != "1"`, bail.

Validation errors are returned as HCL diagnostics pointing at the
offending field's source range.

## 5. Templates — The 80% of Codegen

`text/template` plus `go/format.Source()` is the workhorse pattern.
It's what `stringer`, `protoc-gen-go`, and most `go generate` tools
use. It works well because:

- Templates are readable.
- The output is usually simple enough that you don't need the
  control of AST manipulation.
- `go/format.Source()` at the end means you don't have to care
  about whitespace or indentation in the template.

### 5.1 Embedding Templates

The templates directory ships inside the binary via `//go:embed`:

```go
// internal/gen/render.go
package gen

import (
    "embed"
    "text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

func loadTemplates() (*template.Template, error) {
    return template.New("").
        Funcs(funcMap()).
        ParseFS(templateFS, "templates/*.tmpl")
}
```

No disk layout to depend on at runtime; no template-loading-path
configuration.

### 5.2 A Simple Template — `auth_bearer.go.tmpl`

Generates the bearer-token auth middleware:

```go
{{- /* templates/auth_bearer.go.tmpl */ -}}
// Code generated by mcpgen. DO NOT EDIT.

package mcpauth

import (
    "context"
    "net/http"
    "os"
    "strings"
)

type Subject struct {
    Name string
}

type ctxKey struct{}

func SubjectFromContext(ctx context.Context) (Subject, bool) {
    s, ok := ctx.Value(ctxKey{}).(Subject)
    return s, ok
}

// tokens map: token -> subject name. Parsed from {{.Bearer.TokensEnv}} env var.
var tokens map[string]Subject

func init() {
    tokens = map[string]Subject{}
    for _, pair := range strings.Split(os.Getenv({{printf "%q" .Bearer.TokensEnv}}), ",") {
        pair = strings.TrimSpace(pair)
        if pair == "" { continue }
        parts := strings.SplitN(pair, ":", 2)
        if len(parts) != 2 { continue }
        tokens[parts[1]] = Subject{Name: parts[0]}
    }
}

func Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        h := r.Header.Get("Authorization")
        if !strings.HasPrefix(h, "Bearer ") {
            http.Error(w, "missing bearer token", http.StatusUnauthorized)
            return
        }
        subj, ok := tokens[strings.TrimPrefix(h, "Bearer ")]
        if !ok {
            http.Error(w, "invalid bearer token", http.StatusUnauthorized)
            return
        }
        ctx := context.WithValue(r.Context(), ctxKey{}, subj)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Three things to notice:

1. **The `DO NOT EDIT` header is literally in the template**, not
   added by the renderer. This keeps the template as ground truth;
   reviewing the template shows exactly what ships.
2. **`{{printf "%q" .Bearer.TokensEnv}}` for string embedding.**
   Always use `%q` when embedding user-provided strings into
   generated source — it handles escaping and quoting correctly.
   Never use `"{{.X}}"` unless you've already validated that `X`
   cannot contain quotes, newlines, or backslashes.
3. **`{{-` and `-}}`** trim surrounding whitespace. Makes
   templates survive reformatting.

### 5.3 Template Functions

The rendering setup registers helpers that templates call often:

```go
func funcMap() template.FuncMap {
    return template.FuncMap{
        // Naming conversions — HCL uses snake_case, Go uses PascalCase
        "pascal":   func(s string) string { /* ... */ },
        "camel":    func(s string) string { /* ... */ },
        "snake":    func(s string) string { /* ... */ },

        // Type conversions — HCL's "string"/"number" -> Go types
        "goType":   func(fieldType string) string { /* ... */ },

        // String-quote for embedding user-provided strings safely
        "q":        strconv.Quote,

        // Backticks for when you need a raw string
        "backquote": func(s string) string { return "`" + s + "`" },

        // Does any tool in the list match the predicate?
        "anyProxy": func(tools []ir.Tool) bool {
            for _, t := range tools { if t.Kind == ir.Proxy { return true } }
            return false
        },
    }
}
```

Keep helpers small and obviously-correct. Complex logic belongs in
the renderer before `.Execute`, not inside a template function.

### 5.4 A Harder Template — `tools.go.tmpl`

One tool handler per registered tool. This is where templates
start to earn their keep (and also where they start to get
unwieldy — see §6 on when to use DST instead).

```go
{{- /* templates/tools.go.tmpl */ -}}
// Code generated by mcpgen. DO NOT EDIT.

package mcpserver

import (
    "context"
    "log/slog"
    "time"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/prometheus/client_golang/prometheus"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"

    "{{.Module}}/internal/mcpauth"
    {{- if .HasProxyTools}}
    "{{.Module}}/internal/backend"
    {{- end}}
)

type Tools struct {
    tracer  trace.Tracer
    metrics *Metrics
    {{- if .HasProxyTools}}
    backend *backend.Client
    {{- end}}
}

{{range .Tools}}
// Tool: {{.Name}}
func (t *Tools) {{.Name | pascal}}(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    start := time.Now()
    subj, _ := mcpauth.SubjectFromContext(ctx)

    ctx, span := t.tracer.Start(ctx, "mcp.tool.{{.Name}}",
        trace.WithAttributes(attribute.String("mcp.subject", subj.Name)))
    defer span.End()

    {{range .Inputs}}
    {{- if .Required}}
    {{.Name | camel}}, err := req.Require{{.Type | pascal}}({{.Name | q}})
    if err != nil {
        t.metrics.Invocations.With(prometheus.Labels{
            "tool": {{.Name | q}}, "subject": subj.Name, "outcome": "bad_input",
        }).Inc()
        return mcp.NewToolResultError(err.Error()), nil
    }
    {{- else}}
    {{.Name | camel}} := req.Get{{.Type | pascal}}({{.Name | q}})
    {{- end}}
    {{end}}

    {{if eq .Kind "Proxy"}}
    result, err := t.backend.{{.Name | pascal}}(ctx, {{range $i, $f := .Inputs}}{{if $i}}, {{end}}{{$f.Name | camel}}{{end}})
    {{else}}
    result, err := t.svc.{{.Name | pascal}}(ctx, {{range $i, $f := .Inputs}}{{if $i}}, {{end}}{{$f.Name | camel}}{{end}})
    {{end}}
    if err != nil {
        span.RecordError(err)
        t.metrics.Invocations.With(prometheus.Labels{
            "tool": {{.Name | q}}, "subject": subj.Name, "outcome": "error",
        }).Inc()
        return mcp.NewToolResultError(err.Error()), nil
    }

    t.metrics.Invocations.With(prometheus.Labels{
        "tool": {{.Name | q}}, "subject": subj.Name, "outcome": "success",
    }).Inc()
    t.metrics.Duration.With(prometheus.Labels{
        "tool": {{.Name | q}}, "outcome": "success",
    }).Observe(time.Since(start).Seconds())

    slog.InfoContext(ctx, "mcp tool completed",
        "tool", {{.Name | q}},
        "mcp.subject", subj.Name,
        "outcome", "success",
        "duration_ms", time.Since(start).Milliseconds())

    return mcp.NewToolResultStructured(result), nil
}
{{end}}
```

This is about as complex as a template should get. The `range`
loops repeat one block per tool and one per input. Conditionals
select between proxy and stub call shapes. Helpers (`pascal`,
`camel`, `q`) handle naming and escaping.

### 5.5 Formatting Pass

Templates produce text that is syntactically plausible Go but
probably has messy whitespace — extra blank lines, misaligned
imports, etc. We fix all of that with one call:

```go
// internal/gen/write.go
package gen

import (
    "fmt"
    "go/format"
)

func formatGo(raw []byte) ([]byte, error) {
    formatted, err := format.Source(raw)
    if err != nil {
        // Compile error in the template output. Return the raw bytes
        // with a filename hint so the user can see what we tried to emit.
        return raw, fmt.Errorf("format generated source: %w", err)
    }
    return formatted, nil
}
```

**Critical:** if `format.Source` fails, write the raw bytes to a
`.broken` file alongside the intended path so the user can see
what the template produced. This is how you debug a template
change; otherwise you get "formatting failed" with no artifact.

### 5.6 Unused Imports

Templates often produce code with imports that only some branches
need. Rather than conditional-importing (which is painful), we
import everything we *might* need and suppress unused warnings:

```go
var (
    _ = context.TODO
    _ = time.Second
    _ = prometheus.NewCounterVec
)
```

This is ugly but standard practice in codegen. The alternative is
hundreds of lines of `{{if ...}}` around imports. `goimports` as a
post-pass would also work; we use `format.Source` because it's
stdlib and doesn't need a separate binary.

## 6. When Templates Become Wrong

Templates are wrong for:

- **Branching based on existing code's structure.** Templates only
  know about the IR; they don't know what's already in the target
  `main.go`.
- **Preserving user code across regeneration.** Templates overwrite
  whole files; they can't interleave with hand-written content.
- **Making N edits to an M-line file where N is much less than M.**
  You'd have to template the entire file, which means copying the
  user's code into the template, which means the template now
  owns what the user wrote.

Every one of those describes the embed-mode `main.go` edit. That's
why that single operation uses DST instead.

## 7. DST — The 20%

`github.com/dave/dst` is "Decorated Syntax Tree": it wraps
`go/ast` but keeps comments and spacing attached to nodes, so you
can round-trip existing code without losing formatting. If you've
ever tried to modify Go source with `go/ast` and watched all your
comments move to the wrong lines, DST is the fix.

### 7.1 The Edit We Need

The user's `main.go` has a marker comment:

```go
// mcpgen:hook
```

We need to do two things:

1. Add `"myorg/internal/mcpserver"` to the import block.
2. Insert a call to `mcpserver.Register(...)` on the line
   immediately following the marker comment.

Doing this with string replacement would mostly work but breaks on
edge cases — a commented-out earlier hook, an import alias
collision, a `main()` that uses `// mcpgen:hook` inside a larger
comment block. DST gets each of these right structurally.

### 7.2 Parsing Into DST

```go
// internal/embed/hook.go
package embed

import (
    "github.com/dave/dst"
    "github.com/dave/dst/decorator"
)

func editMainGo(path, module string) error {
    dec := decorator.NewDecorator(nil)
    file, err := dec.ParseFile(path, nil, parser.ParseComments)
    if err != nil {
        return fmt.Errorf("parse %s: %w", path, err)
    }
    // file is a *dst.File with comments/spacing preserved.
    // ...
}
```

The `decorator.NewDecorator` is what attaches "decorations"
(comments and spacing) to nodes.

### 7.3 Adding an Import

DST's `dstutil.AddImport` handles this:

```go
import "github.com/dave/dst/dstutil"

dstutil.AddImport(dec, file, module+"/internal/mcpserver")
```

Internally this finds the existing import block, adds a new
`*dst.ImportSpec`, and keeps alphabetical ordering of imports
where the existing style is alphabetized. If no import block
exists it creates one.

### 7.4 Finding the Marker and Inserting

Walk the AST to find a function declaration named `main`, then
its body's list of statements, then the statement whose leading
comments include `// mcpgen:hook`:

```go
func findHookAndInsert(file *dst.File, module string) error {
    for _, decl := range file.Decls {
        fn, ok := decl.(*dst.FuncDecl)
        if !ok || fn.Name.Name != "main" {
            continue
        }
        for i, stmt := range fn.Body.List {
            if hasHookMarker(stmt) {
                // Build the replacement: the marker comment is a
                // decoration on the NEXT statement (or this one's End).
                // We insert our call AFTER the marker.
                call := buildRegisterCall(module)
                fn.Body.List = append(
                    fn.Body.List[:i+1],
                    append([]dst.Stmt{call}, fn.Body.List[i+1:]...)...,
                )
                return nil
            }
        }
    }
    return fmt.Errorf("marker comment // mcpgen:hook not found in main()")
}

func hasHookMarker(stmt dst.Stmt) bool {
    for _, c := range stmt.Decorations().Start.All() {
        if strings.TrimSpace(c) == "// mcpgen:hook" {
            return true
        }
    }
    return false
}
```

**Decoration attachment points** are DST's name for "where can a
comment live relative to this node." `Decorations().Start` is
"comments that precede this node on their own lines." Others
include `End` (trailing same-line) and various node-specific
points.

### 7.5 Building the Replacement Statement

The call we want to insert is:

```go
if err := mcpserver.Register(ctx, app, cfg); err != nil {
    log.Fatalf("mcp register: %v", err)
}
```

The verbose way to build this in DST is to compose each node by
hand:

```go
call := &dst.IfStmt{
    Init: &dst.AssignStmt{
        Lhs: []dst.Expr{dst.NewIdent("err")},
        Tok: token.DEFINE,
        Rhs: []dst.Expr{
            &dst.CallExpr{
                Fun: &dst.SelectorExpr{
                    X:   dst.NewIdent("mcpserver"),
                    Sel: dst.NewIdent("Register"),
                },
                Args: []dst.Expr{
                    dst.NewIdent("ctx"),
                    dst.NewIdent("app"),
                    dst.NewIdent("cfg"),
                },
            },
        },
    },
    Cond: &dst.BinaryExpr{
        X:  dst.NewIdent("err"),
        Op: token.NEQ,
        Y:  dst.NewIdent("nil"),
    },
    // ... Body with log.Fatalf
}
```

Which is correct but painful. The shorter way is to **parse a
snippet and lift the nodes out:**

```go
snippet := `package p

func _() {
    if err := mcpserver.Register(ctx, app, cfg); err != nil {
        log.Fatalf("mcp register: %v", err)
    }
}`
f, _ := decorator.Parse(snippet)
ifStmt := f.Decls[0].(*dst.FuncDecl).Body.List[0]
```

Parse a throwaway function, extract the statement we want. This is
much easier to read and safer than hand-building, especially for
statements with multiple sub-expressions.

### 7.6 Writing Back

```go
var buf bytes.Buffer
if err := decorator.Fprint(&buf, file); err != nil {
    return err
}
formatted, err := format.Source(buf.Bytes())
if err != nil {
    return fmt.Errorf("format: %w", err)
}
return os.WriteFile(path, formatted, 0o644)
```

`decorator.Fprint` converts the DST back to AST and emits source.
We run `format.Source` afterward as a belt-and-suspenders pass.

### 7.7 Idempotency

Running mcpgen twice should be a no-op on the second run. For the
edit, that means: if the next statement after `// mcpgen:hook` is
already the `mcpserver.Register(...)` call, skip the insert.

```go
if isOurCall(fn.Body.List[i+1]) {
    return nil // already inserted
}
```

`isOurCall` is a structural check: is it an `if err := ...; err !=
nil { ... }` whose `Init` is a call to `mcpserver.Register`? We
don't compare strings; we compare the AST shape.

## 8. The `go generate` Integration

Generated services can be wired into `go generate` so engineers
don't have to remember the mcpgen command:

```go
// cmd/rfc-api-mcp/main.go
//go:generate mcpgen generate --config ../../mcpgen.hcl --mode new --out ../..
```

Running `go generate ./...` re-runs mcpgen. Combined with CI
checks that `go generate` is a no-op (diff against tree), you get
"generated files are always in sync" as a property.

For the `validate` command, a CI check is one line:

```yaml
- run: go install github.com/our-org/mcpgen/cmd/mcpgen@latest
- run: mcpgen validate --config mcpgen.hcl
```

## 9. Testing the Generator

### 9.1 Golden Files

The primary test pattern: for a known input IR, the generated
output is byte-compared to a committed fixture.

```go
// internal/gen/render_test.go
package gen

import (
    "flag"
    "os"
    "path/filepath"
    "testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderToolsProxy(t *testing.T) {
    spec := testSpecWithProxyTools()
    got, err := renderTools(spec)
    if err != nil { t.Fatal(err) }

    golden := filepath.Join("testdata", "golden", "tools_proxy.go")
    if *update {
        os.WriteFile(golden, got, 0o644)
        return
    }

    want, err := os.ReadFile(golden)
    if err != nil { t.Fatal(err) }
    if string(got) != string(want) {
        t.Fatalf("output mismatch:\n--- got\n%s\n--- want\n%s", got, want)
    }
}
```

When a template change is intentional, `go test -update` rewrites
the fixtures. Reviewers check the diff in the fixtures as part of
the PR — that diff is exactly what users will see when they
regenerate.

### 9.2 "Does It Compile?" Tests

For each fixture, after rendering, drop the output in a temp
module and run `go build`:

```go
func TestGeneratedBuilds(t *testing.T) {
    tmp := t.TempDir()
    err := generateInto(testSpec(), tmp)
    if err != nil { t.Fatal(err) }

    cmd := exec.Command("go", "build", "./...")
    cmd.Dir = tmp
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("go build failed:\n%s", out)
    }
}
```

Slow (Go compile) but the thing that catches most regressions.
This is what runs in CI for every PR.

### 9.3 DST Edit Tests

For embed-mode edits, canned `main.go` fixtures before and after:

```go
func TestHookInsertion(t *testing.T) {
    cases := []struct {
        name   string
        before string
        want   string
    }{
        {
            name: "simple",
            before: readFixture("main_simple.before.go"),
            want:   readFixture("main_simple.after.go"),
        },
        {
            name: "with_existing_imports",
            // ...
        },
        {
            name: "marker_comment_missing",
            before: readFixture("main_no_marker.go"),
            want:   "", // edit should error
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got, err := editMain([]byte(tc.before), "myorg")
            // ...
        })
    }
}
```

### 9.4 Regeneration Idempotency

```go
func TestRegenerationIsIdempotent(t *testing.T) {
    tmp := t.TempDir()
    generateInto(testSpec(), tmp)
    snapshot1 := readAllFiles(t, tmp)

    generateInto(testSpec(), tmp) // second run
    snapshot2 := readAllFiles(t, tmp)

    if !reflect.DeepEqual(snapshot1, snapshot2) {
        t.Fatal("second generation produced different output")
    }
}
```

This catches the whole class of bugs where a template emits
`time.Now()` or a random seed and quietly breaks determinism.
Regeneration has to be bit-for-bit.

## 10. Error Handling Philosophy

Three tiers of errors:

1. **Input errors** — bad HCL, missing OpenAPI operation, invalid
   tool name. Always render as HCL diagnostics, pointing at the
   offending line. Exit code 1.
2. **Generator bugs** — template rendering failure, `go/format`
   rejecting our output. These are our bugs; we need to know about
   them. Log the full context (input spec, template name, output
   bytes before formatting) and exit code 2.
3. **Disk / env errors** — can't write, out dir isn't empty. User
   problem; clear message, exit 1.

The CLI should be fast-failing. One bad HCL attribute shouldn't
produce a partially-generated project.

## 11. Things That Will Bite You If You're Not Careful

- **Templates that produce `"fmt"` imports but no `fmt` usage.**
  `format.Source` rejects this as unused. Either use the dummy-
  assign trick (§5.6) or conditional-import in the template.
- **`time.Now()` leaking into generated output.** Every template
  output has to be a pure function of inputs. Timestamps go in
  comments? Never — they break diff stability. If you need a
  generation timestamp in the output, put it in the `mcpgen.hcl`
  as an explicit attribute.
- **Forgetting `%q` on string substitution.** A user's HCL string
  with a double-quote in it becomes an unclosed string literal in
  Go, which `format.Source` catches but only after you've spent
  ten minutes debugging a mysterious error.
- **DST edits on code without the marker comment.** mcpgen errors
  out hard here. Do not be clever and insert at the end of `main`;
  the resulting behavior is unpredictable.
- **Making templates test-unfriendly.** If `renderTools(spec)`
  takes a full HCL file path instead of a ready-built `ir.Spec`,
  you can't table-test it. Keep the rendering functions IR-in,
  bytes-out.
- **Import aliases in the target module for embed mode.** DST's
  `AddImport` handles this if the existing block uses aliases,
  but if your generated call assumes the unaliased name it'll
  fail to compile. Generate the import with an explicit alias
  (`mcpgen_mcpserver "..."`) to sidestep.

## 12. What Part 2 Covers

Part 2 (`using-mcpgen.md`) walks through:

- `mcpgen init` — scaffold a starter HCL for a new service.
- **New-project mode** with a complete example HCL producing a
  full MCP proxy.
- **Embed mode** adding MCP to an existing service.
- **Inline HCL backend** for APIs without an OpenAPI spec.
- **OpenAPI reference backend** for APIs that have one.
- Each of the four auth schemes with their HCL, generated code,
  and deployment considerations.
- Regeneration workflow and CI integration.

If you're building the generator, Part 1 is your reference. If
you're using the generator, jump to Part 2.

## References

- ADR-0001 — mcpgen architectural decisions
- DESIGN-0004 — mcpgen detailed design
- `docs/guide/using-mcpgen.md` — Part 2 of this walkthrough
- `docs/guide/mcp-server-in-go.md` — the patterns mcpgen encodes
- `hashicorp/hcl/v2` docs:
  <https://pkg.go.dev/github.com/hashicorp/hcl/v2>
- `dave/dst` docs: <https://github.com/dave/dst>
- `text/template`: <https://pkg.go.dev/text/template>
- `go/format`: <https://pkg.go.dev/go/format>
