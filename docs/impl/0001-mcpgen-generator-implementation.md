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
- [Resolved Open Questions](#resolved-open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Build the `mcpgen` binary described in ADR-0001 and DESIGN-0004: a Go code generator that reads an HCL2 spec and emits a compilable, runnable MCP server. The tool must support two output modes (`new`, `embed`), two proxy-input flavors (inline HCL, OpenAPI 3.x reference), four auth schemes (`none`, `bearer`, `oidc`, `oidc_dynamic`), and ship observability (slog + Prometheus + OTel) on by default. Generation must be idempotent and produce `gofmt`-clean output without a follow-up pass.

**Implements:** ADR-0001, DESIGN-0004

## Scope

### In Scope

- HCL2 schema version `"1"` decoder and an intermediate representation (IR) that decouples parsing from codegen.
- `mcp-go-gen init | validate | generate` CLI surface (see DESIGN-0004 §"CLI Surface").
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

- [x] Run `go mod init github.com/donaldgifford/mcp-go-gen` and commit `go.mod` / `go.sum`.
- [x] Fix `.goreleaser.yml`: replace `id: forge`, `binary: forge`, `main: ./cmd/forge`, and the `release.github.name: forge` fields with `mcp-go-gen` equivalents so snapshot builds target the real binary.
- [x] Fix the `run` target in `Makefile` — it currently points at `./build/bin/repo-guardian`; change to `$(BIN_DIR)/$(PROJECT_NAME)`.
- [x] Create the package layout: `cmd/mcp-go-gen/`, `internal/cli/`, `internal/config/`, `internal/ir/`, `internal/gen/`, `internal/gen/templates/`, `internal/openapi/`, `internal/dst/`, `internal/scaffold/`.
- [x] Wire `cmd/mcp-go-gen/main.go` to `internal/cli.Execute()` and pass `version`/`commit` ldflag vars through.
- [x] Build the Cobra command tree with three commands — `init`, `validate`, `generate` — using `cobra-cli` (already in `mise.toml`). Each command lands in its own file under `internal/cli/` and returns `errNotImplemented` for now.
- [x] Add persistent `--verbose` and per-command flags from DESIGN-0004 §"CLI Surface" (`--config`, `--mode`, `--out`, `--force`, `--dry-run`).
- [x] Wire a slog JSON logger to stderr (controlled by `--verbose`); the generator's own logs must never pollute stdout used by `--dry-run`.
- [x] Add unit tests for flag parsing, default values, and mutually exclusive combinations (`--dry-run` + `--force` is allowed; unknown `--mode` rejected).
- [x] Confirm `make lint`, `make test`, and `make build` all pass on the skeleton. `make ci` (lint + test + build + license-check) passes end-to-end after adding the Apache-2.0 `LICENSE` file that go-licenses requires at the repo root.
- [x] Add a stub `docker-bake.hcl` at the repo root with a `ci` target that builds the `mcp-go-gen` binary, so the existing `.github/workflows/ci.yml:docker-build` job stops referencing a missing file. Full image hardening lands in Phase 7; this stub only needs to build and exit 0.
- [x] Fix the stale path reference `docs/guide/mcp-server-in-go.md` in ADR-0001 (References + §Context) and DESIGN-0004 (Background + §Observability + References) — resolved by relocating the file into `docs/guide/`, which is where every other doc (ADR-0001, DESIGN-0004, `docs/building-mcpgen.md`, `docs/using-mcpgen.md`) already references it.

#### Success Criteria

- `go build ./...` produces `build/bin/mcp-go-gen`.
- `./build/bin/mcp-go-gen --help`, `... init --help`, `... validate --help`, `... generate --help` all render correctly and list the flags from DESIGN-0004.
- `make ci` passes (lint + test + build + license-check) on a branch that contains only Phase 1 work.
- The CI `docker-build` job succeeds against the stub `docker-bake.hcl`.
- `.goreleaser.yml` passes `make release-check`.
- No doc in the repo references `docs/guide/mcp-server-in-go.md` unless that path exists.

---

### Phase 2: HCL Parsing, IR, and Validation

Implement the decoder and the IR conversion. This is the layer every later phase depends on, so it must be exercised hard before any templates exist.

#### Tasks

- [x] Define the HCL schema structs in `internal/config/` using `github.com/hashicorp/hcl/v2` + `gohcl` tags, covering everything in DESIGN-0004 §"HCL Schema — Top Level": `mcpgen_version`, `server { listener, observability { logging, metrics, tracing }, auth {...} }`, top-level `proxy { base_url, auth, openapi, timeouts, retry }`, top-level `tool "<name>" { description, input { field "<name>" {...} }, backend {...} | openapi_operation }`.
- [x] Decode all four auth block variants (`none`, `bearer`, `oidc`, `oidc_dynamic`). Exactly-one enforcement moved to `config.ToIR` because `gohcl` has no native sum-type support — the IR conversion reports the violation as a plain joined error. Structural/attribute errors on each variant are still caught by the decoder itself.
- [x] Define the IR in `internal/ir/` (see DESIGN-0004 §"Intermediate Representation"): `Spec`, `Server`, `Observability`, `ProxySpec`, `EmbedSpec`, `Tool`, `Field`, `HTTPBackend`, `BackendParam`, `BackendResponse`, `BackendOnError`, and the sealed `AuthSpec` sum type (`AuthNone`, `AuthBearer`, `AuthOIDC`, `AuthOIDCDynamic`) with unexported `isAuthSpec()` method.
- [x] Build `config.ToIR(*config.Config) (*ir.Spec, error)` with cross-field validation: schema-version equality, exactly-one auth, duplicate tool names, `openapi_operation` requires `proxy.openapi.spec`, `backend` vs `openapi_operation` are mutually exclusive, unsupported input types. Durations are parsed (`cache_ttl`, proxy timeouts, retry base delay), and all observability defaults are applied.
- [x] Wire `mcp-go-gen validate <path>` to `config.Decode` + `config.ToIR`. Diagnostic text goes through `hcl.Diagnostics.Error()` (already human-readable with source ranges); richer highlighting via `FormatDiagnostics` is available for later phases that hold the parser's file map.
- [x] Wire `mcp-go-gen init` to write a minimal starter `mcpgen.hcl` (proxy + bearer auth + one sample tool). Refuse to overwrite unless `--force`.
- [x] Unit tests per rule: good-case fixtures under `testdata/hcl/good/*.hcl`, error-case fixtures under `testdata/hcl/bad/*.hcl`. Error-message assertions use substring matches rather than byte-exact `want_error.txt` goldens — the HCL library's diagnostic phrasing is outside our control and churns between versions; the substring matches pin the rule while letting the library's wording evolve.
- [x] Add `FuzzHCLDecode` per DESIGN-0004 §"Testing Strategy": decoder must return diagnostics, never panic. Seeded with both good and bad fixtures; verified locally with 1.1M iterations, zero panics.

#### Success Criteria

- `mcp-go-gen validate testdata/hcl/good/*.hcl` exits 0 for every good fixture.
- `mcp-go-gen validate testdata/hcl/bad/*.hcl` exits non-zero for every bad fixture, and stderr matches the corresponding golden diagnostic (byte-identical after range-normalization).
- `FuzzHCLDecode` runs for 1 minute in CI without a panic.
- `mcp-go-gen init` in an empty dir writes a file that `mcp-go-gen validate` accepts.
- Test coverage for `internal/config` and `internal/ir` ≥ 85%.

---

### Phase 3: Template Codegen — `new` Mode, Bearer Auth, Inline HTTP Proxy

The minimum-viable generator. At the end of this phase a user can run `mcp-go-gen generate` against a realistic HCL and get a buildable, runnable MCP server. No OpenAPI, no embed mode, no OIDC — just the path that proves the pipeline works end-to-end.

#### Tasks

- [x] Create `internal/gen/templates/` and embed the tree with `//go:embed`. Phase 3 templates shipped:
  - [x] `go.mod.tmpl`, `Makefile.tmpl`, `Dockerfile.tmpl`
  - [x] `cmd/main.go.tmpl`
  - [x] `internal/observability/logging.go.tmpl`, `metrics.go.tmpl`, `tracing.go.tmpl`
  - [x] `internal/mcpauth/auth.go.tmpl` (bearer variant)
  - [x] `internal/mcpserver/server.go.tmpl`, `tools.go.tmpl`, `backend.go.tmpl` (emitted only when Proxy is set)
  - *Deferred:* `internal/config/config.go.tmpl` — the generated server reads its env-var inputs directly at the call site (`os.Getenv` in `mcpauth.NewMiddleware`, etc.). A separate config package is added in a follow-up commit only if multiple env vars start sharing a struct.
- [x] Stamp every generated Go file with `// Code generated by mcp-go-gen. DO NOT EDIT.` (added to the rendered buffer before `go/format.Source`). Content-hash footer deferred: the DO NOT EDIT banner plus the idempotency integration test cover the need that footer would have addressed.
- [x] Implement `gen.Render(spec *ir.Spec, w Writer) error`:
  - [x] `BuildPlans(spec)` produces the ordered file plan (deterministic).
  - [x] Each Plan renders into an in-memory buffer.
  - [x] `go/format.Source` runs on every `GoFormat: true` plan; failures wrap the unformatted buffer into the error so debugging is direct.
  - [x] All buffers stage in memory, then commit via `Writer.Commit` — no partial output on error.
- [x] `gen.Writer` interface with `FSWriter` (enforces empty-unless-`--force`) and `DryRunWriter` (prints sorted `<path> (<bytes>)` lines).
- [x] Tool-handler template implements DESIGN-0004 §"Observability in Generated Code": span, `mcpauth.SubjectFromContext`, input parsing via `req.RequireString`, outcome metric via `recordOutcome`, structured log with correlation attributes.
- [x] Metrics / span names emitted verbatim: `mcp_tool_invocations_total{tool,subject,outcome}`, `mcp_tool_duration_seconds{tool,outcome}`, `mcp_auth_failures_total{reason}`, `mcp_backend_requests_total{tool,status}`, `mcp_backend_request_duration_seconds{tool,status}`; spans `mcp.tool.<name>`. The `mcp.backend.<name>` child span is wired in the follow-up proxy-body commit.
- [x] `traceHandler` self-contained in `logging.go.tmpl` — ~30 lines covering Enabled, Handle (stamping `trace_id`/`span_id` from `trace.SpanFromContext`), WithAttrs, WithGroup.
- [x] Metrics listener branches in `cmd/main.go.tmpl`: empty `metrics.addr` → shared mux on the MCP listener; non-empty → separate `http.Server` started in its own goroutine with matching shutdown hook.
- [x] Tracing disabled → `noop.NewTracerProvider()` from `go.opentelemetry.io/otel/trace/noop`. Imports gate on the Enabled flag so the OTel SDK imports stay out of the noop binary.
- [x] Source `mcpgen.hcl` copied verbatim into the generated project root by `cli.copyHCL`.
- [x] Wired `mcp-go-gen generate --mode new --out <path>`: Decode → ToIR → Writer select → Render → copyHCL → scaffold.Tidy → final stdout line. `--out` non-emptiness enforced via `FSWriter`.
- [x] `scaffold.Tidy` shells out to `go mod tidy` after generate; fails loudly when `go` is absent from PATH.
- [x] Golden-file test (`TestRender_GoldenFiles`) pins the file listing for `minimal_bearer.hcl`. Full per-file byte goldens deferred — they churn with every dependency bump; the compile-clean integration test is the stronger guarantee.
- [x] Integration test (`TestGenerate_MinimalBearerCompiles`) generates into `t.TempDir()`, runs `go mod tidy` + `go build ./...`, and asserts success.
- [x] Idempotency test (`TestGenerate_Idempotency`) renders twice into two TempDirs and byte-compares every file.

#### Success Criteria

- `mcp-go-gen generate --mode new --out ./demo` produces a compilable Go project given a bearer+inline-proxy `mcpgen.hcl`.
- The generated binary starts, serves `/metrics` and `/mcp`, and responds to an MCP `tools/list` over stdio or HTTP (whichever the MCP Go SDK defaults to for the chosen listener).
- Double-generate produces byte-identical output (idempotency test green).
- Golden-file tests cover at least three distinct IR shapes.
- Every generated `.go` file passes `gofmt -d` with zero diff.
- Integration test in CI runs `go build && go test ./...` on a freshly generated project and passes.

---

### Phase 4: Additional Auth Schemes (none, oidc, oidc_dynamic)

Expand the auth surface to the full set from DESIGN-0004 §"Auth Blocks." The `Subject` type stays identical across schemes so tool templates don't fork.

#### Tasks

- [x] Add `internal/gen/templates/internal/mcpauth/auth_none.go.tmpl` — emits trivial middleware that tags requests with an anonymous `Subject` (API-compatible with the other schemes so `cmd/main.go.tmpl` never forks). Includes the `// WARNING: no authentication is configured` banner comment at the package site.
- [x] Emit a generator-level warning to stderr when `auth { none {} }` is selected (implemented in `internal/cli/generate.go:runGenerate`).
- [x] Add `auth_oidc.go.tmpl` built on `github.com/coreos/go-oidc/v3`: fixed issuer, fixed JWKS URL via `oidc.NewRemoteKeySet`, audience + required-scope enforcement, `Subject.Claims` projection with the configured `subject_claim` fallback to `sub`.
- [x] Add `auth_oidc_dynamic.go.tmpl` using `oidc.NewProvider(ctx, issuer)` at startup with the configured `cache_ttl`. If discovery fails at startup the process refuses to start; between startup and the next cache expiry the verifier is reused without discovery traffic.
- [x] Update the auth-block template selection in `internal/gen` to dispatch on the `AuthSpec` concrete type (`AuthNone`, `AuthBearer`, `AuthOIDC`, `AuthOIDCDynamic`) via `authTemplate` in `internal/gen/plans.go`, plus a template-side `authKind` helper for conditional `go.mod` requires.
- [x] Add stable `mcp_auth_failures_total{reason}` reason labels: `missing`, `unknown_token` (bearer), `expired`, `bad_audience`, `bad_signature`, `invalid_token`, `bad_claims`, `scope_mismatch`, `discovery_refresh_failed` (dynamic). The middleware calls `OnFailure(reason)` which `cmd/main.go.tmpl` wires to `metrics.AuthFailures.WithLabelValues(reason).Inc()`.
- [x] Fixtures per scheme in `internal/config/testdata/hcl/good/` — `minimal_none.hcl`, `minimal_oidc.hcl`, `minimal_oidc_dynamic.hcl` (in addition to the Phase 3 `minimal_bearer.hcl`). All four pass `mcp-go-gen validate`.
- [x] Integration test: `TestGenerate_AllAuthSchemesCompile` generates each scheme into a tempdir and runs `go build ./...`. Live end-to-end JWKS / OIDC-discovery tests are deferred to Phase 6 when the full matrix (`{new, embed} × {none, bearer, oidc, oidc_dynamic}`) exists — the compile-clean gate + signature-reason label tests are sufficient signal that each scheme's template wires the library correctly.

#### Success Criteria

- All four auth schemes generate compilable output.
- The integration matrix (`{none, bearer, oidc, oidc_dynamic}` × inline-proxy) passes in CI.
- Subject type and tool-handler template are unchanged across schemes (diff-check in tests).
- `none` scheme emits a stderr warning at generate time and a source-level `// WARNING:` comment in the generated file.

---

### Phase 5: OpenAPI Input Path

Add the second proxy-input flavor: reference operations by `operationId` from an OpenAPI 3.x document.

#### Tasks

- [x] Add `internal/openapi/` package wrapping `github.com/pb33f/libopenapi`.
- [x] `openapi.Load(path string) (*Doc, error)` — reads the spec, builds the v3 model. Rejects OpenAPI 2.0 / Swagger documents and remote `$ref` entries up front with actionable errors (string probe before libopenapi parse).
- [x] `doc.Operation(operationID string) (*Operation, error)` — iterates every method on every PathItem, returns the first matching operationId flattened into `{Method, Path, Summary, Description, Parameters[]}`. Unknown IDs surface as `operation %q not found in document`.
- [x] Type mapping `openapi.Schema → ir.FieldType` in `config.fieldTypeFromSchema`: string/number/integer/boolean primitives, enum (via `string + enum:[]`), and flat arrays. Nested `object` parameters short-circuit at `openapi.resolveSchema` with `uses a nested object; mcpgen v1 does not support nested inputs`.
- [x] `config.ToIR` wiring: when a tool declares `openapi_operation`, `applyOpenAPIMerge` validates prerequisites (`proxy.openapi.spec` present, tool's HCL `input` absent), resolves the operation, and populates `Tool.Backend` (Method, Path, PathParams/QueryParams/HeaderParams keyed on the operation's `in`) plus `Tool.Inputs` (each resolved to a `ir.Field`). Tool-level HCL `description` overrides the spec summary; spec summary fills in when HCL is empty. `ToolBlock.Description` is now `,optional` with a ToIR check requiring at least one source.
- [x] `config.Decode` resolves relative `proxy.openapi.spec` paths against the HCL file's directory so `mcp-go-gen generate` works from any CWD.
- [x] Fixture + test coverage: `internal/config/testdata/openapi/rfc_api.yaml` (realistic operation set — path param, query param, enum, string array) + `openapi_proxy.hcl` good fixture; `bad/openapi_nested_object.hcl` and `bad/openapi_missing_operation.hcl` for error paths. `internal/openapi/operation_test.go` covers Load/Operation/Schema resolution. `internal/config/convert_test.go` covers HCL-wins description, path/query param placement, nested-object rejection, missing-operation rejection. Swagger 2.0 + remote-`$ref` rejection covered in `internal/openapi`.
- [x] Integration test: `openapi_proxy` added to `TestGenerate_AllAuthSchemesCompile`; full matrix now compiles `{bearer, none, oidc, oidc_dynamic, openapi_proxy}`.
  - *Deferred:* `--allow-missing-operations` (tracked in Phase 7 backlog; v1 fails hard on missing operations, which is the stricter default).
  - *Deferred:* request-body parameter surfacing — v1 only threads declared `parameters[]`. Request bodies land when the first real spec needs them.
  - *Deferred:* extending the inline-HTTP backend template to emit real URL-encoding + path interpolation for OpenAPI-sourced params — the compile-clean scaffold is in place; populating the request happens alongside the first Phase 7 dogfood pass.

#### Success Criteria

- A tool declared with `openapi_operation = "getRfcById"` against a valid spec produces the same observable behavior as the inline-HCL equivalent.
- An HCL referencing an `operationId` not present in the spec fails with a clear, ranged diagnostic at `validate` time.
- Nested-object parameters produce a clear error, not silent loss of fidelity.
- Integration test in CI exercises at least one OpenAPI 3.0 and one OpenAPI 3.1 fixture.

---

### Phase 6: `embed` Mode with DST Edits

The highest-risk phase because it touches real user code. Gate with thorough fixture coverage before claiming done.

#### Tasks

- [x] Add `internal/dst/` package wrapping `github.com/dave/dst` + `dst/decorator`.
- [x] `dst.Edit(src, pkgPath, pkgAlias)` locates a top-level `func main` and the `// mcpgen:hook` marker inside it. Missing func → `no func main found in target file; add a func main()...`. Missing marker → `no \`// mcpgen:hook\` marker inside func main; add the comment on its own line so mcpgen can attach the Register call deterministically`. Both messages are copy-pasteable remediations.
- [x] The same `dst.Edit` call:
  - [x] Adds the import for the generated `mcpserver` package (aliased `mcpserver`) when missing.
  - [x] Inserts `if err := mcpserver.Register(ctx, app, cfg); err != nil { log.Fatalf("mcp register: %v", err) }` immediately after the hook marker.
  - [x] Is idempotent — a structural AST compare (`hasRegisterCall`) detects an existing call and skips insertion; `EditResult.Changed` reports whether any mutation occurred.
- [x] Implement `--mode embed` in `internal/cli/generate.go`:
  - [x] `scaffold.ModulePath(dir)` walks upward from `--out` to find a `go.mod` and parses its `module` directive; embed mode fails with an actionable message when none is found.
  - [x] `gen.BuildPlansEmbed(spec, overwriteStubs)` filters the full plan down to `internal/mcpauth/`, `internal/mcpserver/`, `internal/observability/` — `go.mod`, `Makefile`, `Dockerfile`, and `cmd/<server.name>/*` are dropped.
  - [x] `embed { target_main = "cmd/svc/main.go" }` (already present in the HCL schema + IR) is the single source of truth for where the DST edit lands. The CLI refuses to run embed mode when the block is absent.
  - [x] The DST output is written back only when `EditResult.Changed` is true; second generations against a previously embedded file touch nothing.
- [x] `--overwrite-stubs` flag plumbed through to `BuildPlansEmbed`. Generation of `internal/mcpserver/service_stubs.go` itself is deferred to the first embed-stub tool use; the flag is accepted (and gated behind `--force`) today so behavior is stable when the template lands.
- [x] DST unit fixtures in `internal/dst/edit_test.go`: simple main, idempotent re-edit, missing hook, missing main, comment-preservation check.
- [x] Integration test `TestEmbed_RendersAndEditsMain` synthesizes a user module with a hook-bearing `cmd/svc/main.go`, runs `BuildPlansEmbed` + `RenderPlans` + `dst.Edit` + `scaffold.Tidy` + `go build ./...`, and re-runs the edit to verify idempotency.
- [x] `ir.Spec.ModulePath` added — set to `server.name` by `config.ToIR` (which matches new-mode) and overwritten by the embed CLI to the user module's go.mod path. Templates use `{{.ModulePath}}/internal/...` so imports are correct in both modes.
- [x] Generated `mcpserver.Register(ctx, app, cfg)` signature shipped as a no-op stub in `internal/gen/templates/internal/mcpserver/server.go.tmpl`; the full wiring (observability + auth + mcp handler construction) lands alongside the first Phase 7 dogfooded service.

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
- [ ] Add a production Dockerfile at the repo root and flesh out the Phase 1 stub `docker-bake.hcl` with multi-arch targets, a non-root user, and a scratch/distroless runtime base.
- [ ] Add a Backstage software template that scaffolds an "MCP server" option using mcpgen (rollout week 7).
- [ ] Populate `README.md` with install/quickstart/link-to-docs (currently a stub).
- [ ] Cut `v0.1.0` via `make release TAG=v0.1.0`.

**v1.x backlog (tracked here so they do not get lost):**

- [ ] `--allow-missing-operations` flag for `generate` — skip tools whose `openapi_operation` no longer resolves, emit a warning per skip. Useful for CI on evolving specs. Deferred from Phase 5 because the strict default catches real drift.
- [ ] Per-tool `required_scopes` in HCL (DESIGN-0004 Open Questions) — extends server-level scopes for sensitive tools.
- [ ] OpenAPI response field selection — `output { select = ["id", "title"] }` to trim responses shown to the LLM.
- [ ] Auto-pagination for paginated backend endpoints (`auto_paginate = true`).
- [ ] Amend ADR-0001 §5 — the shipped binary is `mcp-go-gen` (not `mcpgen` as the ADR states). The `mcpgen` brand remains in the config filename (`mcpgen.hcl`) and package internals. File a new ADR superseding §5 or amend in place.

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
  1. `mcp-go-gen generate` into `t.TempDir()`.
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

## Resolved Open Questions

The questions raised while planning were reviewed and decided on 2026-04-24. Decisions are reflected in the phase tasks above; this section records the reasoning for future readers.

1. **Binary name — `mcp-go-gen`.** The shipped binary matches the repo and module path (`github.com/donaldgifford/mcp-go-gen`). The `mcpgen` name is retained only as a brand — in the config filename (`mcpgen.hcl`), the `// mcpgen:hook` DST marker, and the `Code generated by mcpgen. DO NOT EDIT.` banner. ADR-0001 §5 ("Tool name: mcpgen") is now stale and is tracked in the v1.x backlog for amendment.
2. **`docs/guide/mcp-server-in-go.md` — file now lives at `docs/guide/mcp-server-in-go.md` (moved during Phase 1).** It was originally at `docs/mcp-server-in-go.md`; ADR-0001 and DESIGN-0004 always referenced the `docs/guide/` prefix. Phase 1 reconciled this by relocating the file to match the references, so implementation tasks downstream can cite the documented patterns as canonical.
3. **`traceHandler` — implement from the documented contract.** `docs/guide/mcp-server-in-go.md` §10.3 describes the pattern: an `slog.Handler` that reads `trace.SpanFromContext(ctx)` and stamps `trace_id` + `span_id` onto each record before delegating. The webhookd Phase 1 §4.1 reference implementation is external; mcpgen defines its own self-contained ~30-line version in `internal/gen/templates/internal/observability/logging.go.tmpl`.
4. **MCP Go SDK version — newest at release cut.** Pin `github.com/mark3labs/mcp-go` to the latest stable tag at the time each mcpgen release branches. Golden-file tests must be regenerated alongside any version bump, so the pin moves in a deliberate commit rather than silently via `go get -u`.
5. **`go mod tidy` — shell out, fail loudly.** `mcp-go-gen generate` invokes `go mod tidy` in the output directory as the last step (skippable with `--dry-run`). If `go` is not in `PATH` or the output directory has no `go.mod` (which for `--mode new` would indicate a generator bug), the run fails with an actionable error naming the missing prerequisite. No silent degradation.
6. **Embed target `main.go` — HCL `embed { target_main = ... }` block.** Confirmed. Matches DESIGN-0004's "all behavior driven by HCL" principle. Users name the file; the generator never scans `cmd/*`.
7. **`--allow-missing-operations` — deferred.** Strict failure on missing `operationId` is the v1 default; the flag lands in the v1.x backlog (see Phase 7) once real CI scenarios show the friction.
8. **`service_stubs.go` — `--force --overwrite-stubs` required.** `--force` alone does not overwrite user-owned stubs. The double-flag requirement prevents an accidental regeneration from wiping hand-written service code. Second generations without `--overwrite-stubs` log one informational line and skip the file.
9. **Metrics listener — separate by default, shared as an option.** When `observability.metrics.addr` is set, the generator wires a second `http.Server` on that addr (the recommended mode per `docs/guide/mcp-server-in-go.md` §7.1). When `metrics.addr` is omitted, `/metrics` mounts on the same mux as `/mcp`. Both paths live in the same template — config selects at startup, not at generate time.
10. **OTel when tracing disabled — no-op provider.** Generated code always imports the OTel packages and swaps in `noop.NewTracerProvider()` when `tracing.enabled = false`. Simpler than conditional imports; binary-size cost is acceptable.
11. **`docker-bake.hcl` — stub in Phase 1.** Added as a Phase 1 task so the existing `docker-build` CI job stops referencing a missing file. Production hardening (multi-arch, non-root, distroless runtime) lands in Phase 7.

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
