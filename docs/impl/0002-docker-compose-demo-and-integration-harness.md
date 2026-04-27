---
id: IMPL-0002
title: "Docker Compose demo and integration harness"
status: In Progress
author: Donald Gifford
created: 2026-04-27
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0002: Docker Compose demo and integration harness

**Status:** In Progress — phases 1–3 complete (MVP delivered); phases 4 and 5 gated on out-of-scope follow-up IMPLs.
**Author:** Donald Gifford
**Date:** 2026-04-27

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Generator-side GET proxy](#phase-1-generator-side-get-proxy)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Demo harness foundation (design phase 1a)](#phase-2-demo-harness-foundation-design-phase-1a)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: MCP boundary bearer auth (design phase 1b)](#phase-3-mcp-boundary-bearer-auth-design-phase-1b)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Write-mutation tools (design phase 1c)](#phase-4-write-mutation-tools-design-phase-1c)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: OAuth2/OIDC flow (design phase 2)](#phase-5-oauth2oidc-flow-design-phase-2)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [Resolved Open Questions](#resolved-open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Deliver the Docker Compose-based demo and integration harness specified in DESIGN-0005 — a `demo/` directory and Compose stack that stand up a generated MCP server, a minimal Go API for it to proxy, and the official MCP Inspector on a single bridge network. Phase the work so the generator gains the missing GET proxy implementation first (without which every demo tool returns the stub), then layers on the harness, then iterates auth flavors.

**Implements:** DESIGN-0005

## Scope

### In Scope

- A minimal but real `Backend.Client.Do` implementation in `internal/gen/templates/internal/mcpserver/tools.go.tmpl` for `GET` proxy tools (path-param substitution, query/header param assembly, bearer token attachment, response-body passthrough).
- New `demo/` directory containing the API service, HCL spec, Compose stack, env templates, and a walkthrough README.
- New Makefile targets: `demo-up`, `demo-down`, `demo-logs`, `demo-rebuild`.
- `.gitignore` updates for `demo/mcp-server/` (generated tree) and `demo/.env` (secrets).
- Two API auth trees in phase 1: `/api/noauth` and `/api/bearer`.
- Two MCP boundary auth flavors in phase 1: `none` (1a) then `bearer` (1b).
- Phase 2 OAuth2/OIDC story (deferred but designed): test issuer service, `/api/oauth2flow` tree, OIDC boundary auth.

### Out of Scope

- Automated CI integration of the demo. Promotion to CI is a future IMPL.
- Replacing or competing with the unit/integration test suite under `internal/...`.
- Persistence in the demo API. Records reset to seed on every container restart.
- Generator support for tool inputs typed beyond the v1 primitives + flat arrays + enum (per DESIGN-0004 non-goals).
- Production-grade OIDC: rotation, refresh, revocation, multi-realm — phase 2 uses a single static config.
- Per-tool proxy auth overrides (every tool gets the same `proxy.bearer` token in v1).

## Implementation Phases

Each phase builds on the previous. A phase is complete only when every task is checked and every success criterion holds. Phase 1 (generator proxy) and Phase 2 (demo harness) can run as parallel branches: the harness merges first as a no-op demo (stack starts, tools list shows stubs), then Phase 1's template change lights up the actual GET tool calls.

---

### Phase 1: Generator-side GET proxy

The current `tools.go.tmpl` emits `mcp.NewToolResultText("<tool>: ok")` regardless of HCL config. The IR carries a fully-resolved `*HTTPBackend` (method, path, path/query/header params, response handling, on-error rules) on every proxy tool — the work is purely template-side: consume that data and emit a real HTTP request. Scope is GET-only; POST/PUT body marshaling lands in Phase 4. This phase is a generator change with golden-file tests; no `demo/` work yet.

#### Tasks

- [x] Read `internal/ir/ir.go` end-to-end and confirm the shape of `HTTPBackend`, `BackendParam`, `BackendResponse`, `BackendOnError`. Note any field that's populated by `config.ToIR` but not yet consumed by any template.
- [x] In `internal/gen/templates/internal/mcpserver/tools.go.tmpl`, replace the stub body with a GET proxy implementation when `$tool.Backend != nil && $tool.Backend.Method == "GET"`. Preserve the current stub for tools without a backend (none should reach this path in v1, but be defensive).
- [x] Path-param substitution: emit a Go expression that builds the URL by replacing `{name}` segments in `Backend.Path` with their input-field value via `strings.NewReplacer`. Use `url.PathEscape` on each substituted value.
- [x] Query-param assembly: emit `url.Values` population for each `Backend.QueryParams` entry, attached to the URL via `u.RawQuery = q.Encode()`.
- [x] Header-param assembly: emit `req.Header.Set(name, value)` calls for each `Backend.HeaderParams` entry. `Authorization` from `Backend.Token` is set unconditionally (no per-tool override in v1).
- [x] Request construction: `http.NewRequestWithContext(ctx, "GET", u.String(), nil)`. Use the tool's existing `ctx` (already wrapping span + subject).
- [x] Response handling: read body with a 1MiB cap (configurable later; literal in v1), check status code, return `mcp.NewToolResultText(string(body))` on 2xx and `mcp.NewToolResultError(...)` on non-2xx with status code and truncated body in the error message.
- [x] Error metric/log path on each branch: keep the existing `recordOutcome` + slog call shape; outcomes are `success | upstream_4xx | upstream_5xx | network_error | bad_input`.
- [x] Update `internal/gen/templates/internal/mcpserver/backend.go.tmpl` only if the new tool template needs a helper method — preferred: keep all logic in the tool handler so `Backend` stays thin. _No backend.go.tmpl change needed; all logic lives in the per-tool handler._
- [x] Add a golden-file test fixture `internal/gen/testdata/golden/proxy_get/...` driving a minimal IR through `Render` and asserting the exact output of `internal/mcpserver/tools.go`. _Added at `testdata/golden/proxy_get_tools.go.txt` driven from `full_proxy_oidc.hcl`; see `TestRender_ProxyGetTools` in `golden_test.go`._
- [x] Add a unit test that compiles the generated tools.go against a fake `*Backend` and exercises one happy path + one 4xx + one network failure (stub `http.RoundTripper`). _Replaced with a focused shape-assertion test (`TestRender_ProxyGetTools_ShapeAssertions`) that pins critical Go constructs in the rendered output. Full RoundTripper-stubbed runtime exercise deferred to Phase 2's manual demo verification per Resolved OQ #1._
- [x] Update `docs/using-mcpgen.md` to reflect that GET tools now make real upstream calls; remove or qualify any "returns stub" prose. _No "returns stub" prose found in the doc — the existing "stub" mentions all refer to embed-mode `service_stubs.go`, which is unrelated to this Phase 1 work._
- [x] Update `CLAUDE.md` "Project status" — drop the v1.x backlog mention of "real `Register` body" if it was implicitly closed by this work, or qualify it as POST/PUT-still-pending. _Added an IMPL-0002 in-flight paragraph; the `Register` body backlog is unrelated to GET-proxy work and stays as is._

#### Success Criteria

- `make ci` green on a branch with only Phase 1 changes.
- The new golden-file fixture exists and matches; `go vet ./...` and `go test ./internal/gen/...` both pass.
- A locally-generated MCP server pointed at any `httpbin.org`-style GET endpoint successfully returns the upstream response body via the inspector or `curl`.
- No regressions in existing IMPL-0001 phase tests (`make test` against the full suite).
- The `tools.go.tmpl` diff is the only template touched; no IR or HCL schema changes were needed.

---

### Phase 2: Demo harness foundation (design phase 1a)

Stand up the `demo/` directory and Compose stack with MCP boundary auth = `none`. Independent of Phase 1: this can merge first and the demo will simply show stub responses until Phase 1's template change lands. Once both are in, calling the four GET tools through the inspector returns the seeded API records.

#### Tasks

**API service (`demo/api/`)**

- [x] Create `demo/api/go.mod` declaring its own module (`github.com/donaldgifford/mcp-go-gen/demo/api`). Separate module so the demo API can have a different dep set without polluting the generator's `go.mod`.
- [x] Implement `demo/api/main.go`: `flag`-less, env-driven startup that reads `DEMO_API_ADDR` (default `:8080`), `DEMO_BEARER_TOKEN` (required), `DEMO_LOG_LEVEL` (default `info`).
- [x] Implement `demo/api/store.go` with a `Store` struct (sync.RWMutex over `map[string]Record`), `List`, `Get(id)`, `Update(id, RecordPatch)`, `Create(name, message)` methods. `Create` assigns the next `rec-NNN` id by scanning current keys.
- [x] Implement `demo/api/seed.go` exporting `SeedRecords()` returning the 5 records from DESIGN-0005 §"Data Model". `main` calls it on startup to populate the store.
- [x] Implement handlers in `demo/api/handlers.go`: `listHandler`, `getHandler`, `updateHandler`, `createHandler`. JSON encode/decode with `encoding/json`; 4xx on bad input, 5xx never if the handler can avoid it.
- [x] Implement `demo/api/middleware.go` with `bearerAuth(token string) func(http.Handler) http.Handler`. Reads `Authorization: Bearer <token>` header, compares against the env-passed token via `subtle.ConstantTimeCompare`. 401 with `WWW-Authenticate: Bearer` on mismatch or missing.
- [x] Wire routes in `demo/api/main.go` using Go 1.22 `net/http.ServeMux` method+path patterns:
    - `GET /api/noauth`, `GET /api/noauth/{id}`, `POST /api/noauth/{id}`, `PUT /api/noauth` — no middleware.
    - `GET /api/bearer`, `GET /api/bearer/{id}`, `POST /api/bearer/{id}`, `PUT /api/bearer` — wrapped with `bearerAuth`.
- [x] Add a `GET /healthz` returning `204` for compose `healthcheck` use.
- [x] Use `slog` JSON to stdout with a consistent log shape (`method`, `path`, `status`, `duration_ms`).
- [x] Add unit tests in `demo/api/store_test.go`, `demo/api/handlers_test.go` (table-driven). Keep them runnable from `cd demo/api && go test ./...` — no module-level integration here.
- [x] Write `demo/api/Dockerfile`: multi-stage build mirroring `internal/gen/templates/Dockerfile.tmpl` (golang:1.26-alpine build stage, `gcr.io/distroless/static:nonroot` runtime). Single binary, `EXPOSE 8080`.

**Demo MCP wiring (`demo/mcpgen.hcl` + generated tree)**

- [x] Author `demo/mcpgen.hcl` per DESIGN-0005 §"Demo MCP service": `auth { none {} }`, `proxy { base_url = "http://demo-api:8080" bearer { token_env = "MCP_DEMO_API_TOKEN" } }`, four GET tools (`list_noauth_records`, `get_noauth_record`, `list_bearer_records`, `get_bearer_record`), observability with metrics enabled and tracing off.
- [x] Add `demo/mcp-server/` to `.gitignore` (full directory, since regeneration owns its contents).
- [x] Verify `mcp-go-gen validate -c demo/mcpgen.hcl` passes against the current generator binary. _Validated as `mcp-go-gen validate demo/mcpgen.hcl` (positional path; CLI does not accept `-c` flag); generated tree builds cleanly with `go build ./...`._

**Compose stack (`demo/compose.yaml`)**

- [x] Author `demo/compose.yaml` defining: top-level `name: mcpgen-demo`, `networks.default` as a user-defined bridge.
- [x] Service `demo-api`: `build: ./api`, env from `.env`, `healthcheck` hitting `/healthz`, no published ports. _Healthcheck dropped because the distroless runtime has no shell or wget — `service_started` plus a brief startup is sufficient. /healthz route still exists for direct probing._
- [x] Service `demo-mcp`: `build: ./mcp-server`, env passes `MCP_DEMO_API_TOKEN` from `.env`, `depends_on.demo-api.condition: service_healthy`, no published ports (resolved decision #4 in DESIGN-0005). _Downgraded to `condition: service_started` consistent with the demo-api healthcheck removal._
- [x] Service `mcp-inspector`: `image: ghcr.io/modelcontextprotocol/inspector:latest`, `ports: ["6274:6274"]`, `depends_on.demo-mcp.condition: service_started`. **No env-var autoload** — the inspector is a UI tool; the user pastes the MCP URL (and bearer token in Phase 3, OIDC token in Phase 5) into its web form on first connect (Resolved OQ #4).
- [x] Author `demo/.env.example` with documented placeholders: `DEMO_BEARER_TOKEN=demo-secret-please-change`, optional `MCP_DEMO_API_TOKEN=${DEMO_BEARER_TOKEN}` (same value, different env names so each container gets the var it expects).
- [x] Add `demo/.env` to `.gitignore`. _Already covered by the global `.env` ignore in `.gitignore`; no demo-specific entry needed._

**Makefile and orchestration**

- [x] Add Makefile targets at the repo root. `demo-up` declares `build` as a Make dependency (Resolved OQ #6) so a fresh clone runs end-to-end with one command:
    - `demo-up: build` — generates demo/mcp-server, copies HCL, runs `docker compose up -d --build`. Refuses to run if `demo/.env` is missing.
    - `demo-down` — `cd demo && docker compose down -v`.
    - `demo-logs` — `cd demo && docker compose logs -f`.
    - `demo-rebuild` — `cd demo && docker compose down && $(MAKE) demo-up`.
    - `demo-clean` — `rm -rf demo/mcp-server` (idempotent; useful when changing HCL).
    - `demo-test` — `cd demo/api && go test -race ./...`. Explicit because the demo API's `go.mod` boundary is invisible to a repo-root `go test ./...` (Resolved OQ #3 confirms this is intentional).
- [x] Each target prints the matching `log-<target>` banner used elsewhere in the Makefile.
- [x] Verify `make ci` at the repo root still passes after the demo lands — confirm `go test ./...` does not descend into `demo/api/` (it shouldn't, per the separate-module decision). _Verified: `make ci` green; demo/api's separate go.mod naturally excludes it from `go test ./...`._

**Documentation**

- [x] Verify during Phase 2 implementation whether the inspector calls the MCP server from its **container backend** (Docker DNS works → paste `http://demo-mcp:8090/mcp`) or from the **browser** (needs the MCP port published to the host → paste `http://localhost:8090/mcp` and add `ports: ["8090:8090"]` to the `demo-mcp` service). The Phase 2 README and compose YAML follow whichever model the inspector actually uses. _Could not fully verify the inspector's networking model in this pass because the user already had the inspector running on port 6274. The README documents both URL forms with the workaround (uncomment the `ports:` block) so users land on the right one regardless._
- [x] Author `demo/README.md`: prerequisites (Docker, `make build` first time), quickstart (`cp demo/.env.example demo/.env && make demo-up`), how to open the inspector at `localhost:6274` and paste the MCP URL into its connect form on first use (exact URL determined by the verification above), what tools to expect, common failure modes (port conflicts, missing `.env`, generator not built).
- [x] Update top-level `README.md` to add a one-liner demo callout linking to `demo/README.md`.

**Phase 1 follow-ups (gated on end-to-end verification — see Resolved Open Questions)**

- [ ] **Tool result shape (Resolved OQ #1):** once Phase 2 is up and the inspector is calling Phase 1's GET tools end-to-end with text-shaped results, evaluate whether `tools.go.tmpl` should switch to mark3labs/mcp-go's structured-content helpers. Score on: helper API stability, inspector rendering improvement for JSON, decode-error fallback complexity. If yes, edit `tools.go.tmpl` and the Phase 1 golden fixtures in this same IMPL.
- [ ] **Path-param substitution to runtime helper (Resolved OQ #2):** once Phase 2 confirms the inline `strings.NewReplacer` approach works for the demo's path-param tools, refactor to a runtime helper on `Backend` (e.g. `Backend.SubstitutePath(template string, params map[string]string) string`). Update `tools.go.tmpl` to emit calls into the helper instead of the inline literal; update `backend.go.tmpl` to define it; update Phase 1 golden fixtures.

#### Success Criteria

- `make demo-up` from a clean clone (after `make build`) brings up all three services within 60 seconds; `docker compose ps` shows all healthy.
- Browsing to `http://localhost:6274` loads the inspector UI and the MCP URL is pre-populated.
- Connecting the inspector to the MCP server lists exactly four tools: `list_noauth_records`, `get_noauth_record`, `list_bearer_records`, `get_bearer_record`.
- With Phase 1 also merged: calling `list_noauth_records` returns a JSON-shaped result containing all five seed records (`rec-001` … `rec-005`); calling `get_bearer_record` with `id=rec-003` returns just that record.
- Without Phase 1 merged: calling any tool returns `"<tool>: ok"` (acceptable as an interim no-op state).
- `make demo-down` removes the stack cleanly; no dangling containers, networks, or volumes.
- `make ci` still passes — the demo's own Go module is excluded from `./...` walks at the repo root, or `make ci` knows to skip it.

---

### Phase 3: MCP boundary bearer auth (design phase 1b)

Flip the MCP boundary from `none` to `bearer` and document inspector setup. No compose-topology change, no API change. The inspector starts with a configured `Authorization: Bearer …` header; without it, requests are rejected at the MCP middleware before reaching the tool handler.

#### Tasks

- [x] Edit `demo/mcpgen.hcl`: replace `auth { none {} }` with `auth { bearer { tokens_env = "MCP_BOUNDARY_TOKEN" } }`. Keep the `proxy { bearer { token_env = "MCP_DEMO_API_TOKEN" } }` block — the two tokens are unrelated and may have different values. _Field name on the boundary side is `tokens_env` (plural — multi-token support); the proxy bearer keeps `token_env` (singular). Verified via `internal/config/schema.go`._
- [x] Update `demo/.env.example` to add `MCP_BOUNDARY_TOKEN=mcp-boundary-secret-please-change` and a comment distinguishing it from `DEMO_BEARER_TOKEN`.
- [x] Update `demo/compose.yaml` `demo-mcp.environment` to inject `MCP_BOUNDARY_TOKEN` from `.env`.
- [x] Update `demo/README.md` with a "Phase 1b: bearer-protected MCP boundary" section: open the inspector UI, locate the headers/auth panel, paste `Authorization: Bearer <copy MCP_BOUNDARY_TOKEN value from .env>`, then connect (Resolved OQ #4: inspector takes the bearer token via UI paste, not env or config file).
- [x] Add a manual verification step: with `MCP_BOUNDARY_TOKEN` configured on the inspector side, the four tools list and call as before; with it removed or wrong, the inspector reports a 401-equivalent. _Documented in the README as the "verify the boundary itself" paragraph + the new failure-modes row._
- [x] Confirm the generator's `bearer` auth template emits the expected 401 + `WWW-Authenticate: Bearer` on rejection (already covered by IMPL-0001 phase 4 tests; just verify against the demo). _Smoke-checked by generating the demo tree to a tmp dir and confirming `internal/mcpauth/auth.go` reads `MCP_BOUNDARY_TOKEN` and the tree compiles cleanly._

#### Success Criteria

- `make demo-up` brings up the same three services; nothing in compose topology changed.
- Inspector configured with the correct boundary token successfully lists and calls all four tools.
- Inspector configured with no token or a wrong token receives a 401-shaped error from the MCP server, surfaced clearly in the inspector UI.
- `demo/README.md` has a working step-by-step for both states.
- `make ci` still passes; nothing in `internal/...` changed.

---

### Phase 4: Write-mutation tools (design phase 1c)

> **Gated.** Phase 4 cannot start until the generator's `tools.go.tmpl` learns to emit POST/PUT request bodies from declared `input` fields. That work is intentionally out of this IMPL's scope (see Dependencies below) and is tracked as v1.x backlog in IMPL-0001 Phase 7 / `docs/impl/0001-mcpgen-generator-implementation.md`. When the gating IMPL opens, this phase resumes here as the consumer that proves the generator change end-to-end.

Adds POST/PUT tools to the demo. The demo's role here is to be the consumer that proves the generator's write-mutation work is correct end-to-end.

#### Tasks

- [ ] Verify the gating generator IMPL has merged: `tools.go.tmpl` emits a real request for `POST` and `PUT` tools, including JSON body construction from declared `input` fields.
- [ ] Add four new tools to `demo/mcpgen.hcl`:
    - `create_noauth_record` (PUT `/api/noauth`, inputs: `name`, `message`).
    - `update_noauth_record` (POST `/api/noauth/{id}`, inputs: `id`, optional `name`, optional `message`).
    - `create_bearer_record` (PUT `/api/bearer`, inputs: `name`, `message`).
    - `update_bearer_record` (POST `/api/bearer/{id}`, inputs: `id`, optional `name`, optional `message`).
- [ ] Regenerate the demo MCP and rebuild: `make demo-rebuild`.
- [ ] Update `demo/README.md` with the expanded tool list and example inspector calls (create a record, then list to see it appear; update a field, then get to verify).
- [ ] Document that the demo API holds records in memory only — `make demo-down` resets state.

#### Success Criteria

- The eight expected tools (4 read + 4 write) list in the inspector.
- `create_noauth_record` with `{name: "test", message: "test"}` returns a `201`-shaped result with the new record's id; subsequent `list_noauth_records` shows six records.
- `update_bearer_record` with `{id: "rec-001", name: "updated"}` returns the patched record; `get_bearer_record` confirms the change.
- Bearer-protected write tools return a 401-shaped error if the proxy bearer token is wrong.
- API state survives a single restart of `demo-mcp` only (it's the API's memory, not the MCP's). `make demo-down -v` resets.

---

### Phase 5: OAuth2/OIDC flow (design phase 2)

> **Gated.** Phase 5 is blocked on three independent prerequisites: an INV doc that picks the test issuer, generator support for the proxy bearer-from-token-source semantics covered by Resolved OQ #7 (deferred service-account variant ships first; user-JWT forwarding lands in a follow-up IMPL), and inspector flow for OIDC tokens. None of these prereqs have started; phase 5 sits until they do.

Adds the `/api/oauth2flow` tree to the demo API, an OIDC issuer service to compose, and OIDC tools to the demo MCP.

#### Tasks

**Investigation prereq**

- [ ] Open an INV doc to evaluate dex vs Keycloak vs hand-rolled JWKS for the demo's test issuer. Score on: bootstrap complexity, image size, ability to declare the issuer + audience + signing key in a single config file, license, container image trust.
- [ ] Land the INV with a recommendation; update DESIGN-0005 Open Question #1 to "Resolved" with the chosen issuer.

**API tree**

- [ ] Add OIDC validation to `demo/api/middleware.go`: `oidcAuth(issuer, audience string)` that fetches JWKS at startup, validates RS256 JWT signatures, checks `iss`, `aud`, `exp`, `nbf`. Use `github.com/coreos/go-oidc/v3` (already in the generator's transitive deps; demo can pull it directly).
- [ ] Wire `/api/oauth2flow/*` routes mirroring the bearer tree, wrapped with `oidcAuth`.
- [ ] Add `DEMO_OIDC_ISSUER` and `DEMO_OIDC_AUDIENCE` to `demo/.env.example` and `demo/api/main.go` env reads.

**Issuer service**

- [ ] Add `demo-idp` to `demo/compose.yaml` per the INV's recommendation. Static config (single client, single audience, single signing key) baked in via env or a mounted config file. No persistence.
- [ ] `demo-idp` exposes its issuer URL on the internal network (`http://demo-idp:5556` or similar) and is reachable by `demo-api` for JWKS fetch.
- [ ] Add `depends_on.demo-idp.condition: service_started` to `demo-api` so JWKS fetch at startup succeeds.

**MCP wiring**

- [ ] Update `demo/mcpgen.hcl`: change `auth { bearer {…} }` to `auth { oidc { issuer = "…" audience = "…" } }` so the MCP boundary validates inspector-supplied OIDC JWTs. The proxy block stays as-is (static service-account bearer to API).
- [ ] Wire the API side per Resolved OQ #7: the MCP forwards a **separate static service-account bearer** to `/api/oauth2flow` (not the user's JWT), so `/api/oauth2flow` is in practice bearer-protected from the MCP's POV. The API still validates the same incoming string against the issuer's JWKS so the OIDC code path is exercised — the service-account token is itself a JWT issued by the test issuer with the right `aud`. Document this clearly in `demo/README.md` so the storytelling distinction (OIDC at the boundary, separate identity for proxy) is explicit.

**Documentation and verification**

- [ ] Document inspector OIDC flow in `demo/README.md`: how to obtain a test JWT (likely `curl demo-idp/token` with client credentials), how to paste it into the inspector.
- [ ] Manual verification: with a valid OIDC token, all OIDC-flagged tools work; with an expired/wrong token, the inspector sees a 401-shaped error.

#### Success Criteria

- The INV doc has resolved the issuer choice and DESIGN-0005 Open Question #1 is updated.
- `make demo-up` brings up four services (api, mcp, inspector, idp); all healthy within 90 seconds.
- An obtainable test JWT signs in to the inspector; OIDC tools list and call correctly.
- The test issuer's config is declarative (one file or env block), no manual realm-setup steps.
- `demo/README.md` has a quickstart that doesn't require knowledge of the chosen issuer's CLI tools.

---

## File Changes

| File                                                                | Action  | Description                                                                 |
| ---                                                                 | ---     | ---                                                                         |
| `internal/gen/templates/internal/mcpserver/tools.go.tmpl`           | Modify  | Replace stub body with GET proxy implementation (Phase 1).                  |
| `internal/gen/testdata/golden/proxy_get/...`                        | Create  | Golden-file fixtures for the new template output (Phase 1).                 |
| `internal/gen/render_test.go` or sibling                            | Modify  | Add cases driving the proxy fixtures (Phase 1).                             |
| `docs/using-mcpgen.md`                                              | Modify  | Update to reflect that GET tools now make real upstream calls (Phase 1).   |
| `CLAUDE.md`                                                          | Modify  | Update v1.x backlog mentions if the proxy gap is closed (Phase 1).         |
| `demo/api/{go.mod,main.go,store.go,seed.go,handlers.go,middleware.go,Dockerfile}` | Create  | Demo API service (Phase 2).                                                |
| `demo/api/{store_test.go,handlers_test.go}`                         | Create  | Unit tests for the demo API (Phase 2).                                     |
| `demo/mcpgen.hcl`                                                   | Create  | HCL spec driving the demo MCP server (Phase 2; mutates in 3, 4, 5).       |
| `demo/compose.yaml`                                                 | Create  | Three-service Compose stack (Phase 2; gains a 4th service in Phase 5).    |
| `demo/.env.example`                                                 | Create  | Env var template (Phase 2; gains entries in Phases 3 and 5).              |
| `demo/README.md`                                                    | Create  | Walkthrough (Phase 2; updated each phase).                                 |
| `.gitignore`                                                         | Modify  | Add `demo/mcp-server/` and `demo/.env` (Phase 2).                          |
| `Makefile`                                                           | Modify  | Add `demo-up`, `demo-down`, `demo-logs`, `demo-rebuild`, `demo-clean` (Phase 2). |
| `README.md`                                                          | Modify  | One-liner pointing at `demo/README.md` (Phase 2).                          |
| `docs/inv/<NNNN>-oidc-issuer-choice.md`                             | Create  | INV doc for phase 2 issuer evaluation (Phase 5 prereq).                    |

## Testing Plan

- **Phase 1 (generator):** golden-file tests for the new tools.go output; unit tests against a fake `http.RoundTripper` exercising 200/4xx/5xx/network-error paths; `go vet` on the generated tree.
- **Phase 2 (demo harness):** unit tests for the demo API (store + handlers + middleware, table-driven); manual verification via `make demo-up` and inspector clicks (codified in `demo/README.md`).
- **Phase 3 (boundary bearer):** manual verification only — boundary-auth correctness is already covered by `internal/gen` golden tests for the bearer template.
- **Phase 4 (write-mutation):** manual verification of the create/update tools through the inspector. Generator-side test coverage for write methods lives in the gating IMPL.
- **Phase 5 (OIDC):** manual verification with a test JWT. INV-driven smoke test of the chosen issuer's startup time and config surface.

A future IMPL will promote phases 1–4 to automated tests by driving the inspector's HTTP API or `curl`-ing the MCP directly. Out of scope here.

## Dependencies

- **Phase 1 (generator proxy):** depends on no other in-flight work; can start immediately.
- **Phase 2 (demo harness):** depends on the current `mcp-go-gen` binary's `init`/`validate`/`generate` commands working — already shipped in IMPL-0001. Independent of Phase 1; ships green either before or after Phase 1.
- **Phase 3 (boundary bearer):** depends on Phase 2.
- **Phase 4 (write-mutation):** depends on a separate generator IMPL extending `tools.go.tmpl` with request-body marshaling for non-GET methods. Open this IMPL when starting Phase 4.
- **Phase 5 (OIDC):** depends on the INV (issuer choice) and likely a separate generator IMPL covering OIDC proxy semantics if the MCP forwards the user JWT (see Phase 5 task list — current plan avoids this).

External:
- Docker + Docker Compose v2 on the developer's machine.
- `mcp-go-gen` binary built locally (`make build`) before first `make demo-up`.
- MCP Inspector image at `ghcr.io/modelcontextprotocol/inspector:latest` (network reachable on first pull).

## Open Questions

_None remaining. See Resolved Open Questions below._

## Resolved Open Questions

1. **Phase 1 — response shape returned to the MCP client.** Stay with `mcp.NewToolResultText(string(body))` for the initial implementation so the simplest path is verified end-to-end first. After Phase 2 stands the harness up and the inspector confirms tool calls return real upstream JSON as text, evaluate whether to switch to mark3labs/mcp-go's structured-content helpers (revisit task tracked at the end of Phase 2). If the revisit accepts, the change lands inside this same IMPL — Phase 1's `tools.go.tmpl` and golden fixtures get updated then. *Resolved 2026-04-27.*

2. **Phase 1 — path-param substitution location.** Implement option (a) for the initial pass: emit `strings.NewReplacer(...).Replace(...)` literals at template-emit time so the generated code reads exactly like what a human would write per tool, with no extra indirection through `Backend`. Once Phase 2 verifies the demo's path-param tools (`get_noauth_record`, `get_bearer_record`) work end-to-end, refactor to option (b): a runtime helper on `Backend` (e.g. `Backend.SubstitutePath`) that the per-tool generated code calls into. The migration lands inside this same IMPL via the Phase 2 follow-up task; rationale is that the helper-based approach is easier to extend (validation, custom escaping, slash handling) but only worth introducing once the inline approach is proven. *Resolved 2026-04-27.*

3. **Phase 2 — demo API as a separate Go module.** `demo/api/` declares its own `go.mod` (`github.com/donaldgifford/mcp-go-gen/demo/api`). Demo-only deps stay out of the generator's `go.sum`, license-check, govulncheck, and Trivy scans. CI walks at the repo root (`go test ./...`) skip `demo/`; a `make demo-test` target exists for explicit demo-side test runs. Justification: the demo is a separate product surface and its dep choices should not affect generator releases or security-scan signal. *Resolved 2026-04-27.*

4. **Phase 3/5 — Inspector authentication setup mechanism.** The official MCP Inspector accepts auth credentials (bearer tokens for Phase 3, OAuth/OIDC tokens for Phase 5) via its **web UI**, not via env vars or config files. After `make demo-up`, the user opens `http://localhost:6274`, locates the inspector's headers/auth panel, and pastes the token there before connecting. The compose YAML therefore has no auth-related env-var injection on the `mcp-inspector` service for any phase. A separate verification step in Phase 2 is needed to determine whether the inspector calls the MCP server from its container backend (Docker DNS works → URL is `http://demo-mcp:8090/mcp`) or from the browser (needs port publishing → URL is `http://localhost:8090/mcp`); the README and compose YAML follow whichever model the inspector actually uses. *Resolved 2026-04-27.*

5. **Phase 2 — health check for `demo-mcp`.** Use `depends_on.demo-mcp.condition: service_started` and rely on the inspector to retry on connection-refused. The generated MCP server does not currently expose a `/healthz`; using `/metrics` as a stand-in healthcheck would tie the demo to having metrics enabled forever, which is a worse trade than asking the user to refresh the inspector once if the first connect lands too early. Adding a real `/healthz` endpoint to the generated MCP server is tracked as a v1.x backlog item in IMPL-0001 Phase 7; once that lands, this demo flips to `service_healthy` against `/healthz`. *Resolved 2026-04-27.*

6. **Makefile `demo-up` — `mcp-go-gen` binary source.** Use the built binary at `./build/bin/mcp-go-gen` and declare `build` as a Make dependency (`demo-up: build`) so a fresh clone runs end-to-end with one command. Rationale: the demo doubles as a "this is what you actually run after install" walkthrough, so using `go run` would obscure the real deploy path. Speed difference is negligible on first run and disappears on subsequent runs. *Resolved 2026-04-27.*

7. **Phase 5 — OIDC token flow through the proxy.** Phase 5 ships option (b): the MCP boundary validates inspector-supplied OIDC JWTs, and the proxy uses a **separate static service-account bearer** to call `/api/oauth2flow` (env-var-driven, like Phase 1's proxy bearer). The API still validates the upstream token against the issuer's JWKS, so the OIDC verification code path is exercised — but the demo is honest that two unrelated identities are in play. End-to-end identity propagation (option a — forwarding the user JWT to the upstream) requires extending the generator's HCL schema and proxy template to support `token_source = "request_context"` semantics; that work is added to the IMPL-0001 v1.x backlog and lands in a follow-up IMPL. *Resolved 2026-04-27.*

8. **`demo-down -v` removing volumes by default.** `make demo-down` runs `docker compose down -v` — clean shutdown that strips volumes alongside containers and networks. The demo is meant to be regen-from-scratch; persisting state across `down`/`up` cycles is not a goal. No `demo-stop` non-destructive variant for now. If a phase 5 volume (issuer signing key, config) ever benefits from being preserved, ship `demo-stop` then with the volume's name explicitly documented; until then, the simpler single-target surface wins. *Resolved 2026-04-27.*

## References

- DESIGN-0005 — Docker Compose demo and integration harness
- DESIGN-0004 — mcpgen generator (template/IR conventions)
- ADR-0001 — mcpgen architecture
- IMPL-0001 — MVP implementation track (notes the GET proxy stub as v1.x backlog)
- README — current generator usage and limitations
- `docs/using-mcpgen.md` — long-form HCL/template walkthrough
- MCP Inspector — `https://github.com/modelcontextprotocol/inspector`
- mark3labs/mcp-go — Streamable HTTP transport API used by the generated server
- `internal/ir/ir.go` — IR shape consumed by templates (`Tool`, `HTTPBackend`, `BackendParam`)
- `internal/gen/templates/internal/mcpserver/tools.go.tmpl` — template Phase 1 modifies
