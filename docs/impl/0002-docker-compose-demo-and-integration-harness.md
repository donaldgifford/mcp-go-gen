---
id: IMPL-0002
title: "Docker Compose demo and integration harness"
status: Draft
author: Donald Gifford
created: 2026-04-27
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0002: Docker Compose demo and integration harness

**Status:** Draft
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

- [ ] Read `internal/ir/ir.go` end-to-end and confirm the shape of `HTTPBackend`, `BackendParam`, `BackendResponse`, `BackendOnError`. Note any field that's populated by `config.ToIR` but not yet consumed by any template.
- [ ] In `internal/gen/templates/internal/mcpserver/tools.go.tmpl`, replace the stub body with a GET proxy implementation when `$tool.Backend != nil && $tool.Backend.Method == "GET"`. Preserve the current stub for tools without a backend (none should reach this path in v1, but be defensive).
- [ ] Path-param substitution: emit a Go expression that builds the URL by replacing `{name}` segments in `Backend.Path` with their input-field value via `strings.NewReplacer`. Use `url.PathEscape` on each substituted value.
- [ ] Query-param assembly: emit `url.Values` population for each `Backend.QueryParams` entry, attached to the URL via `u.RawQuery = q.Encode()`.
- [ ] Header-param assembly: emit `req.Header.Set(name, value)` calls for each `Backend.HeaderParams` entry. `Authorization` from `Backend.Token` is set unconditionally (no per-tool override in v1).
- [ ] Request construction: `http.NewRequestWithContext(ctx, "GET", u.String(), nil)`. Use the tool's existing `ctx` (already wrapping span + subject).
- [ ] Response handling: read body with a 1MiB cap (configurable later; literal in v1), check status code, return `mcp.NewToolResultText(string(body))` on 2xx and `mcp.NewToolResultError(...)` on non-2xx with status code and truncated body in the error message.
- [ ] Error metric/log path on each branch: keep the existing `recordOutcome` + slog call shape; outcomes are `success | upstream_4xx | upstream_5xx | network_error | bad_input`.
- [ ] Update `internal/gen/templates/internal/mcpserver/backend.go.tmpl` only if the new tool template needs a helper method — preferred: keep all logic in the tool handler so `Backend` stays thin.
- [ ] Add a golden-file test fixture `internal/gen/testdata/golden/proxy_get/...` driving a minimal IR through `Render` and asserting the exact output of `internal/mcpserver/tools.go`.
- [ ] Add a unit test that compiles the generated tools.go against a fake `*Backend` and exercises one happy path + one 4xx + one network failure (stub `http.RoundTripper`).
- [ ] Update `docs/using-mcpgen.md` to reflect that GET tools now make real upstream calls; remove or qualify any "returns stub" prose.
- [ ] Update `CLAUDE.md` "Project status" — drop the v1.x backlog mention of "real `Register` body" if it was implicitly closed by this work, or qualify it as POST/PUT-still-pending.

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

- [ ] Create `demo/api/go.mod` declaring its own module (`github.com/donaldgifford/mcp-go-gen/demo/api`). Separate module so the demo API can have a different dep set without polluting the generator's `go.mod`.
- [ ] Implement `demo/api/main.go`: `flag`-less, env-driven startup that reads `DEMO_API_ADDR` (default `:8080`), `DEMO_BEARER_TOKEN` (required), `DEMO_LOG_LEVEL` (default `info`).
- [ ] Implement `demo/api/store.go` with a `Store` struct (sync.RWMutex over `map[string]Record`), `List`, `Get(id)`, `Update(id, RecordPatch)`, `Create(name, message)` methods. `Create` assigns the next `rec-NNN` id by scanning current keys.
- [ ] Implement `demo/api/seed.go` exporting `SeedRecords()` returning the 5 records from DESIGN-0005 §"Data Model". `main` calls it on startup to populate the store.
- [ ] Implement handlers in `demo/api/handlers.go`: `listHandler`, `getHandler`, `updateHandler`, `createHandler`. JSON encode/decode with `encoding/json`; 4xx on bad input, 5xx never if the handler can avoid it.
- [ ] Implement `demo/api/middleware.go` with `bearerAuth(token string) func(http.Handler) http.Handler`. Reads `Authorization: Bearer <token>` header, compares against the env-passed token via `subtle.ConstantTimeCompare`. 401 with `WWW-Authenticate: Bearer` on mismatch or missing.
- [ ] Wire routes in `demo/api/main.go` using Go 1.22 `net/http.ServeMux` method+path patterns:
    - `GET /api/noauth`, `GET /api/noauth/{id}`, `POST /api/noauth/{id}`, `PUT /api/noauth` — no middleware.
    - `GET /api/bearer`, `GET /api/bearer/{id}`, `POST /api/bearer/{id}`, `PUT /api/bearer` — wrapped with `bearerAuth`.
- [ ] Add a `GET /healthz` returning `204` for compose `healthcheck` use.
- [ ] Use `slog` JSON to stdout with a consistent log shape (`method`, `path`, `status`, `duration_ms`).
- [ ] Add unit tests in `demo/api/store_test.go`, `demo/api/handlers_test.go` (table-driven). Keep them runnable from `cd demo/api && go test ./...` — no module-level integration here.
- [ ] Write `demo/api/Dockerfile`: multi-stage build mirroring `internal/gen/templates/Dockerfile.tmpl` (golang:1.26-alpine build stage, `gcr.io/distroless/static:nonroot` runtime). Single binary, `EXPOSE 8080`.

**Demo MCP wiring (`demo/mcpgen.hcl` + generated tree)**

- [ ] Author `demo/mcpgen.hcl` per DESIGN-0005 §"Demo MCP service": `auth { none {} }`, `proxy { base_url = "http://demo-api:8080" bearer { token_env = "MCP_DEMO_API_TOKEN" } }`, four GET tools (`list_noauth_records`, `get_noauth_record`, `list_bearer_records`, `get_bearer_record`), observability with metrics enabled and tracing off.
- [ ] Add `demo/mcp-server/` to `.gitignore` (full directory, since regeneration owns its contents).
- [ ] Verify `mcp-go-gen validate -c demo/mcpgen.hcl` passes against the current generator binary.

**Compose stack (`demo/compose.yaml`)**

- [ ] Author `demo/compose.yaml` defining: top-level `name: mcpgen-demo`, `networks.default` as a user-defined bridge.
- [ ] Service `demo-api`: `build: ./api`, env from `.env`, `healthcheck` hitting `/healthz`, no published ports.
- [ ] Service `demo-mcp`: `build: ./mcp-server`, env passes `MCP_DEMO_API_TOKEN` from `.env`, `depends_on.demo-api.condition: service_healthy`, no published ports (resolved decision #4 in DESIGN-0005).
- [ ] Service `mcp-inspector`: `image: ghcr.io/modelcontextprotocol/inspector:latest`, env autoload of the MCP URL, `ports: ["6274:6274"]`, `depends_on.demo-mcp.condition: service_started`.
- [ ] Author `demo/.env.example` with documented placeholders: `DEMO_BEARER_TOKEN=demo-secret-please-change`, optional `MCP_DEMO_API_TOKEN=${DEMO_BEARER_TOKEN}` (same value, different env names so each container gets the var it expects).
- [ ] Add `demo/.env` to `.gitignore`.

**Makefile and orchestration**

- [ ] Add Makefile targets at the repo root:
    - `demo-up` — `$(MAKE) build && ./build/bin/mcp-go-gen generate -c demo/mcpgen.hcl -o demo/mcp-server && cp demo/mcpgen.hcl demo/mcp-server/ && cd demo && docker compose up -d --build`.
    - `demo-down` — `cd demo && docker compose down -v`.
    - `demo-logs` — `cd demo && docker compose logs -f`.
    - `demo-rebuild` — `cd demo && docker compose down && $(MAKE) demo-up`.
    - `demo-clean` — `rm -rf demo/mcp-server` (idempotent; useful when changing HCL).
- [ ] Each target prints the matching `log-<target>` banner used elsewhere in the Makefile.

**Documentation**

- [ ] Author `demo/README.md`: prerequisites (Docker, `make build` first time), quickstart (`cp demo/.env.example demo/.env && make demo-up`), how to open the inspector at `localhost:6274`, what tools to expect, common failure modes (port conflicts, missing `.env`, generator not built), opt-in for publishing `demo-mcp:8090` to host.
- [ ] Update top-level `README.md` to add a one-liner demo callout linking to `demo/README.md`.

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

- [ ] Edit `demo/mcpgen.hcl`: replace `auth { none {} }` with `auth { bearer { token_env = "MCP_BOUNDARY_TOKEN" } }`. Keep the `proxy { bearer { token_env = "MCP_DEMO_API_TOKEN" } }` block — the two tokens are unrelated and may have different values.
- [ ] Update `demo/.env.example` to add `MCP_BOUNDARY_TOKEN=mcp-boundary-secret-please-change` and a comment distinguishing it from `DEMO_BEARER_TOKEN`.
- [ ] Update `demo/compose.yaml` `demo-mcp.environment` to inject `MCP_BOUNDARY_TOKEN` from `.env`.
- [ ] Update `demo/README.md` with a "Phase 1b: bearer-protected MCP boundary" section: how to configure the inspector to send the `Authorization` header (which inspector UI field, env var, or config file controls this — depends on Open Question #4).
- [ ] Add a manual verification step: with `MCP_BOUNDARY_TOKEN` configured on the inspector side, the four tools list and call as before; with it removed or wrong, the inspector reports a 401-equivalent.
- [ ] Confirm the generator's `bearer` auth template emits the expected 401 + `WWW-Authenticate: Bearer` on rejection (already covered by IMPL-0001 phase 4 tests; just verify against the demo).

#### Success Criteria

- `make demo-up` brings up the same three services; nothing in compose topology changed.
- Inspector configured with the correct boundary token successfully lists and calls all four tools.
- Inspector configured with no token or a wrong token receives a 401-shaped error from the MCP server, surfaced clearly in the inspector UI.
- `demo/README.md` has a working step-by-step for both states.
- `make ci` still passes; nothing in `internal/...` changed.

---

### Phase 4: Write-mutation tools (design phase 1c)

Adds POST/PUT tools to the demo. Gated on a separate generator IMPL (out of this doc's scope) that extends `tools.go.tmpl` with request-body marshaling for non-GET methods. The demo's role here is to be the consumer that proves the generator's write-mutation work is correct end-to-end.

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

Adds the `/api/oauth2flow` tree to the demo API, an OIDC issuer service to compose, and OIDC tools to the demo MCP. Gated on (a) a phase 2 design refinement that picks the issuer (Open Question #1 below), (b) generator support for OIDC `proxy` blocks emitting bearer-from-token-source (Open Question #2 below), and (c) inspector support for sending OIDC tokens (likely a manual paste from `kubectl get token`-style helpers).

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

- [ ] Update `demo/mcpgen.hcl`: split `auth { bearer {…} }` into a separate config or document that phase 2 uses `auth { oidc { issuer = "…" audience = "…" } }`. The proxy block stays as-is (bearer to API); the boundary auth swaps in OIDC.
- [ ] Decide: does the MCP itself proxy the OIDC token through to `/api/oauth2flow`, or use a separate service-account bearer for the proxy call? In v1 we have one proxy bearer; the simpler path is a separate static service-account token validated by the API as a bearer (treating `/api/oauth2flow` as bearer-protected from the MCP's POV, and the OIDC verification lives only at the MCP boundary). Document the choice clearly.

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

1. **Phase 1 — response shape returned to the MCP client.** The generator's existing `mcp.NewToolResultText(string(body))` flattens the upstream JSON into a string the inspector renders as a code block. Mark3labs's mcp-go also exposes `mcp.NewToolResultJSON` (or equivalent structured-content helpers — name TBD pending a check of the SDK's current API) which the inspector could render structured. Choosing structured may be more useful for inspector users but ties the generator to a specific SDK shape. **Default proposal:** stay with `NewToolResultText` for v1; revisit if/when mcp-go's structured-content helpers stabilize.

2. **Phase 1 — path-param substitution location.** Two options: (a) substitute at template-emit time using Go template logic, producing a `strings.NewReplacer` literal in the generated code; (b) emit a tiny helper function once per tool that does substitution at runtime. (a) is faster at runtime and easier to read in the diff; (b) is easier to extend with escaping/validation later. **Default proposal:** (a) for v1.

3. **Phase 2 — demo API as a separate Go module.** The plan above gives `demo/api/` its own `go.mod` so it can have a different dep set without polluting the generator's module. Alternative: keep it in the main module under `demo/api/`, gated by a build tag. Separate-module is cleaner but means `make ci` at the repo root needs to know not to walk into `demo/`, and a developer running `go test ./...` from the repo root won't pick up demo API tests. **Default proposal:** separate module, Makefile `demo-test` target for explicit invocation; CI ignores `demo/`.

4. **Phase 3 — Inspector bearer setup mechanism.** I have not verified what the official MCP Inspector accepts as configuration for an `Authorization: Bearer` header on outgoing MCP requests. Options seen in similar tools include: (a) UI-side input field, (b) env var like `MCP_AUTH_TOKEN`, (c) a config file mounted at a known path, (d) a query string param. The phase-3 task list assumes one of these is available; verifying which is a prerequisite to writing the README walkthrough. **Action:** confirm against `github.com/modelcontextprotocol/inspector` README before starting Phase 3 tasks.

5. **Phase 2 — health check for `demo-mcp`.** The generated MCP server doesn't currently expose a `/healthz` endpoint, only `/metrics` (when enabled). Compose's `depends_on.demo-mcp.condition: service_healthy` would therefore need a healthcheck added either to the HCL schema (long path) or by hitting `/metrics` with a `200`-or-better status (works today). **Default proposal:** use `service_started` for inspector→demo-mcp, and let the inspector retry on connection refusals. If retries are flaky, add a `healthcheck` against `/metrics` as a workaround.

6. **Makefile `demo-up` — `mcp-go-gen` binary source.** The plan calls `./build/bin/mcp-go-gen` (so `make build` is a prereq). Alternative: `go run ./cmd/mcp-go-gen` (works without `make build` but slower per-invocation). **Default proposal:** require `make build` first; `demo-up` declares `build` as a Make dependency so it's transparent.

7. **Phase 5 — OIDC token flow through the proxy.** If the MCP forwards the inspector's user JWT to `/api/oauth2flow`, the generator needs an OIDC-aware `proxy` block (token-from-context). Otherwise the MCP uses a separate service-account bearer. Plan above takes the second path to avoid extending the generator. **Action:** confirm during Phase 5 design refinement whether token-forwarding is a goal.

8. **`demo-down -v` removing volumes by default.** Phase 2 currently has no volumes, so `-v` is a no-op. Phase 5's issuer service may want a volume for its config or signing key. **Default proposal:** `demo-down` strips volumes (`-v`) since the demo is meant to be regen-from-scratch; document a `demo-stop` target without `-v` if a non-destructive stop is wanted later.

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
