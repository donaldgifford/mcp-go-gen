---
id: IMPL-0001
title: "mcpgen Generator Implementation"
status: Draft
author: Donald Gifford
created: 2026-04-24
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0001: mcpgen Generator Implementation

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-24

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Repo Foundation & CLI Skeleton](#phase-1-repo-foundation--cli-skeleton)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: HCL Parsing, IR, and Validation](#phase-2-hcl-parsing-ir-and-validation)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Template Codegen — new Mode, Bearer Auth, Inline HTTP Proxy](#phase-3-template-codegen--new-mode-bearer-auth-inline-http-proxy)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Additional Auth Schemes (none, oidc, oidc_dynamic)](#phase-4-additional-auth-schemes-none-oidc-oidcdynamic)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: OpenAPI Input Path](#phase-5-openapi-input-path)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
  - [Phase 6: embed Mode with DST Edits](#phase-6-embed-mode-with-dst-edits)
    - [Tasks](#tasks-5)
    - [Success Criteria](#success-criteria-5)
  - [Phase 7: Dogfooding, Release Hardening, Distribution](#phase-7-dogfooding-release-hardening-distribution)
    - [Tasks](#tasks-6)
    - [Success Criteria](#success-criteria-6)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Build the `mcpgen` binary described in ADR-0001 and DESIGN-0004: a Go code generator that reads an HCL2 spec and emits a compilable, runnable MCP server. The tool must support two output modes (`new`, `embed`), two proxy-input flavors (inline HCL, OpenAPI 3.x reference), four auth schemes (`none`, `bearer`, `oidc`, `oidc_dynamic`), and ship observability (slog + Prometheus + OTel) on by default. Generation must be idempotent and produce `gofmt`-clean output without a follow-up pass.

**Implements:** ADR-0001, DESIGN-0004

## Scope

### In Scope

- HCL2 schema version `"1"` decoder and an intermediate representation (IR) that decouples parsing from codegen.
- `mcpgen init | validate | generate` CLI surface (see DESIGN-0004 §"CLI Surface").
- `text/template` + `go/format.Source()` pipeline for new files.
- `github.com/dave/dst` pipeline for the single embed-mode edit to a user's `main.go` at the `// mcpgen:hook` marker.
- `github.com/pb33f/libopenapi` integration for OpenAPI 3.0/3.1 operation lookup.
- All four auth schemes, all three tool flavors (inline-HTTP proxy, OpenAPI proxy, embed stub).
- Generated-code observability: slog JSON, `/metrics` Prometheus endpoint (optionally on a separate listener), OTLP/HTTP tracing exporter.
- Golden-file tests for every template × IR combination; end-to-end `go build && go test` on generated output in CI.
- Idempotent regeneration (byte-identical output for identical input).

### Out of Scope

- Non-HCL config input (YAML/JSON wrappers are the user's problem).
- Non-Go generated output (Python/TS/Rust).
- Runtime config reloading in generated services.
- Business-logic synthesis — only wiring, arg parsing, and backend calls are generated.
- OpenAPI 2.0 / Swagger, remote `$ref` over HTTP, gRPC inputs.
- The webhookd Phase 3 MCP server (hand-written, per DESIGN-0004 non-goals).
- Nested-object tool inputs (flat types + arrays only in v1).
- Per-tool scope overrides, OpenAPI response field selection, auto-pagination (deferred to v1.1).

## Implementation Phases

Each phase builds on the previous. A phase is complete only when every task is checked and every success criterion holds. Phases are sized to be independently shippable — the generator should produce useful output at the end of Phase 3 and grow capabilities from there.

---

### Phase 1: Repo Foundation & CLI Skeleton

Bring the scaffold from "no Go code" to "builds a binary with a working command surface." This phase fixes template leftovers from the repo scaffold and establishes the package layout the later phases fill in.

#### Tasks

- [ ] Run `go mod init github.com/donaldgifford/mcp-go-gen` and commit `go.mod` / `go.sum`.
- [ ] Fix `.goreleaser.yml`: replace `id: forge`, `binary: forge`, `main: ./cmd/forge`, and the `release.github.name: forge` fields with `mcp-go-gen` equivalents so snapshot builds target the real binary.
- [ ] Fix the `run` target in `Makefile` — it currently points at `./build/bin/repo-guardian`; change to `$(BIN_DIR)/$(PROJECT_NAME)`.
- [ ] Create the package layout: `cmd/mcp-go-gen/`, `internal/cli/`, `internal/config/`, `internal/ir/`, `internal/gen/`, `internal/gen/templates/`, `internal/openapi/`, `internal/dst/`, `internal/scaffold/`.
- [ ] Wire `cmd/mcp-go-gen/main.go` to `internal/cli.Execute()` and pass `version`/`commit` ldflag vars through.
- [ ] Build the Cobra command tree with three commands — `init`, `validate`, `generate` — using `cobra-cli` (already in `mise.toml`). Each command lands in its own file under `internal/cli/` and returns `errNotImplemented` for now.
- [ ] Add persistent `--verbose` and per-command flags from DESIGN-0004 §"CLI Surface" (`--config`, `--mode`, `--out`, `--force`, `--dry-run`).
- [ ] Wire a slog JSON logger to stderr (controlled by `--verbose`); the generator's own logs must never pollute stdout used by `--dry-run`.
- [ ] Add unit tests for flag parsing, default values, and mutually exclusive combinations (`--dry-run` + `--force` is allowed; unknown `--mode` rejected).
- [ ] Confirm `make lint`, `make test`, and `make build` all pass on the skeleton.

#### Success Criteria

- `go build ./...` produces `build/bin/mcp-go-gen`.
- `./build/bin/mcp-go-gen --help`, `... init --help`, `... validate --help`, `... generate --help` all render correctly and list the flags from DESIGN-0004.
- `make ci` passes (lint + test + build + license-check) on a branch that contains only Phase 1 work.
- `.goreleaser.yml` passes `make release-check`.

---

### Phase 2: HCL Parsing, IR, and Validation

Implement the decoder and the IR conversion. This is the layer every later phase depends on, so it must be exercised hard before any templates exist.

#### Tasks

- [ ] Define the HCL schema structs in `internal/config/` using `github.com/hashicorp/hcl/v2` + `gohcl` tags, covering everything in DESIGN-0004 §"HCL Schema — Top Level": `mcpgen_version`, `server { listener, observability { logging, metrics, tracing }, auth {...} }`, top-level `proxy { base_url, auth, openapi, timeouts, retry }`, top-level `tool "<name>" { description, input { field "<name>" {...} }, backend {...} | openapi_operation }`.
- [ ] Decode all four auth block variants (`none`, `bearer`, `oidc`, `oidc_dynamic`) and enforce "exactly one" at decode time with a `hcl.Diagnostic` that points at the offending source range.
- [ ] Define the IR in `internal/ir/` (see DESIGN-0004 §"Intermediate Representation"): `Spec`, `Server`, `Observability`, `ProxySpec`, `Tool`, `Field`, `HTTPBackend`, and the `AuthSpec` sum type with `isAuthSpec()` sealed method.
- [ ] Build `config.ToIR(*config.Config) (*ir.Spec, error)` with all cross-field validation: duplicate tool names; `openapi_operation` set but no top-level `proxy.openapi.spec`; `backend` block present in `embed`-only contexts; unsupported input types; `mcpgen_version` must equal `"1"` (anything else → clear error with upgrade guidance).
- [ ] Wire `mcpgen validate <path>` to `config.Decode` + `ir.Validate` and print HCL diagnostics with their source ranges using `hcl.NewDiagnosticTextWriter`.
- [ ] Wire `mcpgen init` to write a minimal starter `mcpgen.hcl` (proxy + bearer auth + one sample tool). Refuse to overwrite unless `--force`.
- [ ] Unit tests per rule: good-case fixtures under `testdata/hcl/good/*.hcl`, error-case fixtures under `testdata/hcl/bad/*.hcl` with `want_error.txt` golden files for diagnostics.
- [ ] Add `FuzzHCLDecode` per DESIGN-0004 §"Testing Strategy": decoder must return diagnostics, never panic.

#### Success Criteria

- `mcpgen validate testdata/hcl/good/*.hcl` exits 0 for every good fixture.
- `mcpgen validate testdata/hcl/bad/*.hcl` exits non-zero for every bad fixture, and stderr matches the corresponding golden diagnostic (byte-identical after range-normalization).
- `FuzzHCLDecode` runs for 1 minute in CI without a panic.
- `mcpgen init` in an empty dir writes a file that `mcpgen validate` accepts.
- Test coverage for `internal/config` and `internal/ir` ≥ 85%.

---

### Phase 3: Template Codegen — `new` Mode, Bearer Auth, Inline HTTP Proxy

The minimum-viable generator. At the end of this phase a user can run `mcpgen generate` against a realistic HCL and get a buildable, runnable MCP server. No OpenAPI, no embed mode, no OIDC — just the path that proves the pipeline works end-to-end.

#### Tasks

- [ ] Create `internal/gen/templates/` and embed the tree with `//go:embed`. Author the minimum set of templates for a bearer+proxy new-project output, matching DESIGN-0004 §"Generated File Layout":
  - [ ] `go.mod.tmpl`, `Makefile.tmpl`, `Dockerfile.tmpl`
  - [ ] `cmd/{{.Server.Name}}/main.go.tmpl`
  - [ ] `internal/config/config.go.tmpl`
  - [ ] `internal/observability/logging.go.tmpl`, `metrics.go.tmpl`, `tracing.go.tmpl`
  - [ ] `internal/mcpauth/auth.go.tmpl` (bearer variant)
  - [ ] `internal/mcpserver/server.go.tmpl`, `tools.go.tmpl`
  - [ ] `internal/backend/client.go.tmpl` (inline HTTP)
- [ ] Stamp every generated file with `// Code generated by mcpgen. DO NOT EDIT.` and a content-hash footer for idempotency debugging.
- [ ] Implement `internal/gen.Render(spec *ir.Spec, out gen.Writer) error`:
  - [ ] Build a per-file plan from the IR (filename → template name → context).
  - [ ] Render each to a buffer.
  - [ ] Run `go/format.Source()` on every `.go` file; fail loud with the original buffer if formatting fails.
  - [ ] Write to disk only after all files render successfully (no partial output on error).
- [ ] Implement the `gen.Writer` abstraction with two concrete implementations: `FSWriter` (real filesystem with `--force` semantics) and `DryRunWriter` (prints planned actions to stdout).
- [ ] Implement the tool-handler template per DESIGN-0004 §"Observability in Generated Code": tracer span, subject from context, input parsing, backend call, metrics recording, structured logging with trace correlation.
- [ ] Emit the metrics and span names from DESIGN-0004 §"Observability in Generated Code" verbatim (`mcp_tool_invocations_total`, `mcp_tool_duration_seconds`, `mcp_auth_failures_total`, `mcp_backend_requests_total`, `mcp_backend_request_duration_seconds`; spans `mcp.tool.<name>`, `mcp.backend.<name>`).
- [ ] Copy the source `mcpgen.hcl` into the generated project root verbatim (byte-for-byte).
- [ ] Wire `mcpgen generate --mode new --out <path>` to the full pipeline; enforce `--out` must not exist or must be empty unless `--force`.
- [ ] After generation, run `go mod tidy` in the output directory unless `--dry-run`. Capture its stderr and surface failures.
- [ ] Golden-file tests under `internal/gen/testdata/golden/`: for each fixture IR, a directory of expected output files, byte-compared. `go test -update` regenerates.
- [ ] Integration test: generate a bearer+inline-proxy project into `t.TempDir()`, run `go build ./...` in it, assert success.
- [ ] Regeneration idempotency test: generate twice into the same output dir with `--force`; diff must be empty.

#### Success Criteria

- `mcpgen generate --mode new --out ./demo` produces a compilable Go project given a bearer+inline-proxy `mcpgen.hcl`.
- The generated binary starts, serves `/metrics` and `/mcp`, and responds to an MCP `tools/list` over stdio or HTTP (whichever the MCP Go SDK defaults to for the chosen listener).
- Double-generate produces byte-identical output (idempotency test green).
- Golden-file tests cover at least three distinct IR shapes.
- Every generated `.go` file passes `gofmt -d` with zero diff.
- Integration test in CI runs `go build && go test ./...` on a freshly generated project and passes.

---

### Phase 4: Additional Auth Schemes (none, oidc, oidc_dynamic)

Expand the auth surface to the full set from DESIGN-0004 §"Auth Blocks." The `Subject` type stays identical across schemes so tool templates don't fork.

#### Tasks

- [ ] Add `internal/gen/templates/internal/mcpauth/auth_none.go.tmpl` — emits no middleware but leaves `SubjectFromContext` returning an anonymous subject. Include the `// WARNING: no authentication configured` banner comment.
- [ ] Emit a generator-level warning to stderr when `auth { none {} }` is selected.
- [ ] Add `auth_oidc.go.tmpl` built on `github.com/coreos/go-oidc/v3`: fixed issuer, fixed JWKS URL, audience + required scope enforcement, subject-claim projection to the common `Subject` struct.
- [ ] Add `auth_oidc_dynamic.go.tmpl` using `oidc.NewProvider(ctx, issuer)` at startup with the configured `cache_ttl`. Surface a startup failure path — if discovery fails, the process refuses to start (matches webhookd Phase 1 convention).
- [ ] Update the auth-block template selection in `internal/gen` to dispatch on the `AuthSpec` concrete type (`AuthNone`, `AuthBearer`, `AuthOIDC`, `AuthOIDCDynamic`).
- [ ] Add `mcp_auth_failures_total{reason}` incrementation for each rejection path: missing token, bad signature, wrong audience, scope mismatch, expired.
- [ ] Golden-file tests: one fixture per scheme; each must produce compilable output.
- [ ] Integration tests per scheme:
  - [ ] `none`: request without credentials succeeds.
  - [ ] `bearer`: valid token in `MCP_TOKENS` env → success; invalid → 401 + metric increment.
  - [ ] `oidc`: spin an in-process mock JWKS server, sign a JWT, assert success; tamper → failure.
  - [ ] `oidc_dynamic`: same as `oidc` but the mock server publishes `/.well-known/openid-configuration`.

#### Success Criteria

- All four auth schemes generate compilable output.
- The integration matrix (`{none, bearer, oidc, oidc_dynamic}` × inline-proxy) passes in CI.
- Subject type and tool-handler template are unchanged across schemes (diff-check in tests).
- `none` scheme emits a stderr warning at generate time and a source-level `// WARNING:` comment in the generated file.

---

### Phase 5: OpenAPI Input Path

Add the second proxy-input flavor: reference operations by `operationId` from an OpenAPI 3.x document.

#### Tasks

- [ ] Add `internal/openapi/` package wrapping `github.com/pb33f/libopenapi`.
- [ ] `openapi.Load(path string) (*Doc, error)` — reads the spec, resolves local `$ref`, returns a handle. Reject 2.0, reject remote `$ref` (explicit error, not silent drop).
- [ ] `doc.Operation(operationID string) (*Operation, error)` — returns method, path, parameters, request body schema, responses.
- [ ] In `config.ToIR`, when a tool declares `openapi_operation`, resolve it against the top-level `proxy.openapi.spec` and merge the operation's parameters + response shape into the tool's IR. Tool-level HCL overrides (e.g., richer `description`) win over spec values.
- [ ] Implement type mapping from OpenAPI schema types → IR input types (`string`, `number`, `boolean`, `enum`, flat arrays). Reject nested objects with a clear error: "OpenAPI operation <id> uses a nested object for parameter <name>; mcpgen v1 does not support nested inputs."
- [ ] Extend the inline-HTTP backend template to handle OpenAPI-sourced path/query/header parameter placement.
- [ ] Golden-file tests: fixture HCL + fixture OpenAPI spec (small, hand-written) → expected generated output.
- [ ] Integration test: generate against a realistic OpenAPI (e.g., a trimmed Petstore) → compiles and starts.
- [ ] Error-path tests: missing `operationId`, renamed operation, nested-object parameter, OpenAPI 2.0 document, external-URL `$ref`.
- [ ] Add the `--allow-missing-operations` flag (deferred from DESIGN-0004 Open Questions) — or explicitly decide not to and move that item to Phase 7's follow-ups list. See open question below.

#### Success Criteria

- A tool declared with `openapi_operation = "getRfcById"` against a valid spec produces the same observable behavior as the inline-HCL equivalent.
- An HCL referencing an `operationId` not present in the spec fails with a clear, ranged diagnostic at `validate` time.
- Nested-object parameters produce a clear error, not silent loss of fidelity.
- Integration test in CI exercises at least one OpenAPI 3.0 and one OpenAPI 3.1 fixture.

---

### Phase 6: `embed` Mode with DST Edits

The highest-risk phase because it touches real user code. Gate with thorough fixture coverage before claiming done.

#### Tasks

- [ ] Add `internal/dst/` package wrapping `github.com/dave/dst` + `dst/decorator`.
- [ ] Implement `dst.FindHook(file *dst.File) (position, error)` that locates the `// mcpgen:hook` comment inside a function named `main`. If not found, return a structured error suggesting the user add the marker with a copy-pasteable snippet.
- [ ] Implement `dst.InsertRegisterCall(file *dst.File, pkgPath, pkgAlias string) error` that:
  - [ ] Adds an import for the generated package (using the configured alias).
  - [ ] Inserts `if err := mcpserver.Register(ctx, app, cfg); err != nil { log.Fatalf("mcp register: %v", err) }` immediately after the hook marker.
  - [ ] Is idempotent — running twice on a file that already has the call is a no-op (structural compare of the inserted AST node against what's already present).
- [ ] Implement `--mode embed` in `internal/cli/generate.go`:
  - [ ] Require `--out` to point at an existing module (a `go.mod` must be walkable upward from the path).
  - [ ] Generate only the `internal/mcpauth/`, `internal/mcpserver/`, and relevant `internal/observability/` files; skip `go.mod`, `Makefile`, `Dockerfile`, `cmd/*`.
  - [ ] Locate the target `main.go` via a new HCL field (`embed { target_main = "cmd/svc/main.go" }`) — the user names the file; the generator never guesses. See open question.
  - [ ] Apply the DST edit; write via `go/format.Source()` on the final buffer.
- [ ] Generate `internal/mcpserver/service_stubs.go` containing empty `ServiceFunc_*` functions for every embed-stub tool; this file is hand-written territory after first generation — never overwrite unless `--force` *and* a user confirmation flag.
- [ ] Add fixture `main.go` files under `internal/dst/testdata/`: simple main, main with existing imports, main with the hook already inserted (idempotency), main without hook (error), main with multiple functions named `main` (shouldn't happen but assert clear error).
- [ ] Integration test: generate embed into a fresh module, run `go build ./...`, assert the binary starts.

#### Success Criteria

- DST edit preserves all comments, formatting, and blank-line structure in the unedited regions of `main.go` (verified by diff-ignoring-inserted-lines).
- Idempotency: applying the edit twice produces identical source.
- Missing `// mcpgen:hook` → hard error with actionable instructions and non-zero exit; no partial edit.
- `service_stubs.go` is never overwritten on a second generation unless the user explicitly opts in.
- Integration matrix now includes `{new, embed} × {none, bearer, oidc, oidc_dynamic} × {proxy-inline, proxy-openapi, embed-stub}` per DESIGN-0004 §"Testing Strategy"; all combinations build and pass their smoke tests.

---

### Phase 7: Dogfooding, Release Hardening, Distribution

Ship it. Catch the problems that only show up when a real service goes through the pipe.

#### Tasks

- [ ] Generate an MCP frontend for the markdown RFC API (per DESIGN-0004 rollout week 6). Compare field-by-field against the walkthrough-based hand-written equivalent; file issues for every divergence and fix them.
- [ ] Generate an embed-mode MCP surface for a real service with an existing `main.go`; confirm the DST edit on a non-toy target.
- [ ] Audit every generator error message: each must name the HCL source range when applicable and suggest a remediation.
- [ ] Document the HCL schema end-to-end in `docs/using-mcpgen.md` (already scaffolded) and ensure the examples in `building-mcpgen.md` reflect the final generated output.
- [ ] Publish the binary via goreleaser. Confirm `.goreleaser.yml` targets `cmd/mcp-go-gen` (fixed in Phase 1) and builds for `linux`/`darwin` × `amd64`/`arm64`.
- [ ] Add a Dockerfile at the repo root and wire a `docker-bake.hcl` so CI's `docker-build` job has something to build (currently references a file that does not exist).
- [ ] Add a Backstage software template that scaffolds an "MCP server" option using mcpgen (rollout week 7).
- [ ] Populate `README.md` with install/quickstart/link-to-docs (currently a stub).
- [ ] Cut `v0.1.0` via `make release TAG=v0.1.0`.

#### Success Criteria

- `make ci` passes with zero errors.
- Test coverage ≥ 80% across `internal/config`, `internal/ir`, `internal/gen`, `internal/openapi`, `internal/dst`.
- A real service (markdown RFC API) ships with a mcpgen-generated MCP frontend in production.
- `v0.1.0` goreleaser artifacts build, sign, and publish successfully.
- The Backstage template creates a working MCP project in under five minutes, end-to-end.

---

## File Changes

Every path below is relative to the repo root. "Create" means the file does not exist at the start of the phase.

| File | Phase | Action | Description |
|------|-------|--------|-------------|
| `go.mod`, `go.sum` | 1 | Create | Module init; dependencies added incrementally. |
| `.goreleaser.yml` | 1 | Modify | Replace `forge` leftovers with `mcp-go-gen`. |
| `Makefile` | 1 | Modify | Fix `run` target path. |
| `cmd/mcp-go-gen/main.go` | 1 | Modify | Wire to `internal/cli.Execute`. |
| `internal/cli/{root,init,validate,generate}.go` | 1 | Create | Cobra command tree. |
| `internal/config/{schema,decode}.go` | 2 | Create | HCL structs + decoder. |
| `internal/ir/{ir,validate,convert}.go` | 2 | Create | IR types and config→IR conversion. |
| `testdata/hcl/{good,bad}/*.hcl` | 2 | Create | Decoder fixtures. |
| `internal/gen/{render,writer,plan}.go` | 3 | Create | Template rendering pipeline. |
| `internal/gen/templates/**` | 3–6 | Create | Embedded templates; grows per phase. |
| `internal/gen/testdata/golden/**` | 3–6 | Create | Golden output per fixture IR. |
| `internal/openapi/{load,operation,types}.go` | 5 | Create | libopenapi wrapper. |
| `internal/dst/{find,insert,idempotent}.go` | 6 | Create | DST edit helpers. |
| `internal/dst/testdata/main_*.go` | 6 | Create | Edit fixtures. |
| `internal/scaffold/tidy.go` | 3 | Create | `go mod tidy` runner. |
| `Dockerfile`, `docker-bake.hcl` | 7 | Create | Satisfies CI `docker-build` job. |
| `README.md` | 7 | Modify | Replace stub. |
| `docs/using-mcpgen.md`, `docs/building-mcpgen.md` | 7 | Modify | Align to shipped generator. |

## Testing Plan

- **Unit** — per package, table-driven where possible. Targets: `internal/config`, `internal/ir`, `internal/gen` (pure rendering), `internal/openapi`, `internal/dst`.
- **Golden files** — `internal/gen/testdata/golden/<fixture>/` holds the expected directory tree per IR fixture. `go test -update` regenerates. Every template change must regenerate goldens in the same commit.
- **Fuzz** — `FuzzHCLDecode` on the config parser; runs in CI for 1 minute, locally much longer.
- **Integration** — the matrix from DESIGN-0004 §"Testing Strategy": `{new, embed} × {none, bearer, oidc, oidc_dynamic} × {proxy-inline, proxy-openapi, embed-stub}`. Each cell:
  1. `mcpgen generate` into `t.TempDir()`.
  2. `go build ./...` in the output.
  3. Start the binary on a random port (where applicable).
  4. Connect the MCP Inspector SDK client; assert `tools/list` returns the expected set; call one tool and assert observability metrics increment.
- **Idempotency** — every integration test runs generate twice and diffs the output; must be empty.
- **DST safety** — fixture `main.go` files exercised with `go/parser.ParseFile` after edit to ensure syntactic validity, plus diff-ignoring-inserted-lines to prove surrounding code is untouched.
- **Coverage target** — ≥ 80% on core packages; enforced in CI via `.codecov.yml`.

## Dependencies

**External Go modules the generator itself uses:**

- `github.com/spf13/cobra` — CLI.
- `github.com/hashicorp/hcl/v2` + `github.com/hashicorp/hcl/v2/gohcl` — HCL parsing.
- `github.com/dave/dst` + `github.com/dave/dst/decorator` — Go source edits.
- `github.com/pb33f/libopenapi` — OpenAPI 3.x parsing.

**External Go modules the generated code pulls in** (managed via templates):

- `github.com/mark3labs/mcp-go` — MCP server SDK.
- `github.com/prometheus/client_golang` — metrics.
- `go.opentelemetry.io/otel` and OTLP/HTTP exporter — tracing.
- `github.com/coreos/go-oidc/v3` — OIDC (Phase 4 only).

**License gate** — all of the above must appear in the allowed list in `make license-check` (Apache-2.0 / MIT / BSD / ISC / MPL-2.0). Any new indirect dependency that trips the check blocks the merge.

**Tooling** pinned in `mise.toml`: Go 1.26.1, golangci-lint 2.11.4, goreleaser, mockery, goimports, go-licenses.

## Open Questions

These are implementation-level questions that came up while planning the phases. Answers will change the work in the indicated phase. Each has my current best guess; flag any you want to decide differently before I start.

1. **Module name vs. binary name.** The repo is `mcp-go-gen`, ADR-0001 §5 says "Tool name: `mcpgen`," and the Makefile binary is `$(PROJECT_NAME) = mcp-go-gen`. Should the shipped binary be `mcpgen` (matching the ADR) and the module stay `github.com/donaldgifford/mcp-go-gen`? That's the direction I planned for Phase 1, but confirm before I rename.
2. **Where does `docs/guide/mcp-server-in-go.md` live?** DESIGN-0004 §"Observability in Generated Code" cites it as the source of the tool-handler pattern, and ADR-0001 references it in its References section, but it is not in this repo. Is it in a sibling repo (`fwsync`? the webhookd tree?), or does it need to be imported into `docs/guide/` as part of Phase 1? Phase 3's template fidelity depends on having that pattern in hand.
3. **Reference for the slog trace handler.** DESIGN-0004 §"Observability in Generated Code" says the generated code uses "the same `traceHandler` slog wrapper pattern from webhookd Phase 1." Same question — is that code available to crib from, or does mcpgen need to define the handler in its templates from scratch?
4. **MCP Go SDK pinning.** ADR-0001 names `mark3labs/mcp-go`; DESIGN-0004 uses `mcp.CallToolRequest`, `mcp.NewToolResultStructured`, `req.RequireString`. I need to pin a specific version in the generator templates so that golden files are stable. Which version? (Right now I'd pick "latest as of branch creation" and re-pin at each generator release.)
5. **`go mod tidy` after generate: in-process or separate?** DESIGN-0004 §"Generated File Layout" notes `go.sum` is "generated via `go mod tidy` at end." Does mcpgen shell out to `go mod tidy` as part of `generate`, fail the run on its error, and require Go in PATH — or does it emit a message telling the user to run it? My current plan is to shell out (with the option to skip via `--no-tidy`); flag if you'd rather it not depend on `go` being present.
6. **Embed mode: how does the user point at `main.go`?** I planned an HCL `embed { target_main = "cmd/svc/main.go" }` block, but ADR-0001 and DESIGN-0004 don't specify the mechanism — they just say "the designated `main.go`." Alternatives: a CLI flag (`--target-main`), or a convention ("first `cmd/*/main.go` under `--out`"). HCL feels most consistent with "all behavior is driven by the HCL config" from DESIGN-0004 §"CLI Surface"; confirm.
7. **`--allow-missing-operations` now or later?** DESIGN-0004 lists it under Open Questions, not under the v1 Rollout. I'd defer it to Phase 7's follow-ups rather than build it in Phase 5. OK?
8. **`service_stubs.go` protection.** For embed mode, the stub file is user-owned after first generation. DESIGN-0004 says mcpgen never overwrites hand-written files. I planned to make `--force` *alone* insufficient for this file — it would need `--force --overwrite-stubs` (or similar) to prevent foot-gun. Is that the right friction, or should `--force` be enough?
9. **Metrics listener: shared vs. separate.** HCL allows `metrics.addr` to differ from the main listener. The generated code needs to handle both cases (shared mux vs. separate `http.Server`). I'd prefer to always generate the separate-server path and conditionally start it; alternative is two different templates. Opinion?
10. **OTLP defaults when tracing is disabled.** If `observability.tracing.enabled = false`, should the tracer be a no-op provider (clean, but still imports the OTel packages) or should the import be entirely omitted via template conditionals (smaller binary but template becomes conditional-heavy)? I lean toward no-op provider for simplicity; flag if binary size matters.
11. **Docker build in CI.** `.github/workflows/ci.yml` has a `docker-build` job that references `docker-bake.hcl`, which does not exist in the repo. Phase 1 will fail that job until I add a stub. Should I add the `docker-bake.hcl` in Phase 1 (to keep CI green) and flesh it out in Phase 7, or disable that job until Phase 7?

## References

- [ADR-0001](../adr/0001-mcpgen-architecture.md) — architectural decisions for mcpgen
- [DESIGN-0004](../design/0004-mcpgen-generator.md) — detailed design of the mcpgen generator
- `docs/building-mcpgen.md` — walkthrough, Part 1: building the generator
- `docs/using-mcpgen.md` — walkthrough, Part 2: using the generator across modes and auth schemes
- HCL2: <https://github.com/hashicorp/hcl>
- `dave/dst`: <https://github.com/dave/dst>
- `pb33f/libopenapi`: <https://github.com/pb33f/libopenapi>
- `text/template`: <https://pkg.go.dev/text/template>
- `go/format`: <https://pkg.go.dev/go/format>
- `mark3labs/mcp-go`: <https://github.com/mark3labs/mcp-go>
- `coreos/go-oidc`: <https://github.com/coreos/go-oidc>
