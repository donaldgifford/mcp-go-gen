# mcp-go-gen demo

A self-contained Docker Compose stack that walks `mcp-go-gen` end-to-end: the
**generated MCP server**, a small hand-written **Go HTTP API** for it to proxy,
and the official **MCP Inspector** UI — all on one bridge network so the
inspector can reach the generated server without `host.docker.internal`
gymnastics.

This is the IMPL-0002 deliverable. See:

- `docs/design/0005-docker-compose-demo-and-integration-harness.md` — design.
- `docs/impl/0002-docker-compose-demo-and-integration-harness.md` — phased
  implementation plan and resolved decisions.

## What's in here

| Path              | What it is                                                                                                                                                                                     |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `api/`            | The minimal Go HTTP API (separate Go module). Five seed records, three auth trees (`/api/noauth`, `/api/bearer`, `/api/oauth2flow`); all three are wired.                                      |
| `idp/`            | The hand-rolled OIDC issuer (separate Go module). RS256 JWTs over `/token`, JWKS over `/jwks.json`. Per INV-0001 we ship this rather than dex/Keycloak — see the INV for the trade-off matrix. |
| `mcpgen.hcl`      | Default HCL spec — auth = `bearer` (phase 1b/1c). Eight tools across the noauth + bearer trees.                                                                                                |
| `mcpgen-oidc.hcl` | Phase 5 HCL variant — auth = `oidc` against `demo-idp`. Four tools across the oauth2flow tree. Switched in via `make demo-up-oidc`.                                                            |
| `mcp-server/`     | Generated each `make demo-up` from `mcpgen.hcl`. **Gitignored** — never edit by hand.                                                                                                          |
| `compose.yaml`    | Three services (`demo-api`, `demo-mcp`, `mcp-inspector`) on one bridge.                                                                                                                        |
| `.env.example`    | Template for `demo/.env` (copy, edit, never commit).                                                                                                                                           |

## Quickstart

```bash
# 1. Copy the env template and edit if you want non-default tokens.
cp demo/.env.example demo/.env

# 2. Bring it up. Generates the MCP server tree, builds all three
#    images, starts the stack detached.
make demo-up

# 3. Open the inspector UI.
open http://localhost:6274
```

In the inspector UI (Phase 1b — MCP boundary auth = `bearer`):

1. Locate the inspector's headers/auth panel **before** connecting. Add a
   request header:
   - **Header name:** `Authorization`
   - **Header value:**
     `Bearer <copy the token portion of MCP_BOUNDARY_TOKEN from demo/.env>`

   For the default `.env.example` token, the full value reads:

   ```
   Bearer mcp-boundary-secret-please-change
   ```

   > [!WARNING]
   > **The `Bearer` prefix is required.** The MCP Inspector sends
   > the custom-header value verbatim — it does NOT add `Bearer` for you. If you
   > paste only the token, `demo-mcp` rejects the request with 401 and the
   > inspector escalates to OAuth-discovery (which fails with a confusing "OAuth
   > Authentication Failed" / "404 page not found" message because the
   > bearer-mode MCP server doesn't expose OAuth endpoints). Always include
   > `Bearer` and the trailing space.
   >
   > Likewise, paste **only the token portion** — not the full `.env` value. The
   > `MCP_BOUNDARY_TOKEN=demo-user:mcp-boundary-secret-please-change` entry uses
   > `<subject>:<token>` format on the server side; the inspector sends just the
   > token, and `demo-mcp` looks up the matching subject from the parsed map.

   The MCP server reads `MCP_BOUNDARY_TOKEN` from its container env (injected by
   `compose.yaml` from `demo/.env`), parses it as `<subject>:<token>` pairs, and
   accepts the token portion on /mcp. Without a matching `Bearer <token>` header
   the MCP server rejects the connection at its middleware before any tool
   handler runs.

2. Connect to the demo MCP server. **Verify which URL form works for your
   inspector build** — see [Inspector URL caveat](#inspector-url-caveat) below.
3. The tools list should show eight tools — four read + four write:
   - `list_noauth_records`, `get_noauth_record`, `list_bearer_records`,
     `get_bearer_record` (GET).
   - `create_noauth_record`, `update_noauth_record`, `create_bearer_record`,
     `update_bearer_record` (PUT/POST).
4. Call `list_noauth_records` (no input). Expect a JSON list of the five seed
   records.
5. Call `get_bearer_record` with `id=rec-003`. Expect that single record
   returned as JSON.
6. Call `create_noauth_record` with `name="demo"`, `message="hello"`. Expect a
   201-shaped result with the new record's id (`rec-006`); re-call
   `list_noauth_records` to see the six records.
7. Call `update_bearer_record` with `id=rec-001`, `name="updated"` (omit
   `message` — `RecordPatch` keeps the prior value when a field is absent).
   Expect the patched record back; re-call `get_bearer_record` with `id=rec-001`
   to verify.

To verify the boundary itself: clear the `Authorization` header (or paste a
wrong value) and reconnect — the inspector should surface a 401-shaped error
before any tool list call lands. This exercises the generator's bearer auth
middleware (covered by IMPL-0001 phase 4 golden tests).

> **Two perimeters, two tokens.** `MCP_BOUNDARY_TOKEN` gates inspector → MCP.
> `DEMO_BEARER_TOKEN` (mirrored to demo-mcp as `MCP_DEMO_API_TOKEN`) gates MCP →
> `/api/bearer/*`. Either can rotate independently. They default to different
> values in `.env.example` for that reason.

```bash
# Tail the stack logs to see what's happening.
make demo-logs

# Tear down (removes containers + networks + volumes).
make demo-down
```

## Inspector URL caveat

The inspector ships as a single container with both UI and MCP-client code.
Whether the MCP request originates from the **container backend** (Docker DNS
resolves `demo-mcp`) or from the **browser** (only sees `localhost`) depends on
the inspector's internals.

- If `http://demo-mcp:8090/mcp` connects on first try → done.
- If it fails with a connection error, the browser is making the request
  directly. Edit `compose.yaml` and uncomment the `ports: ["8090:8090"]` block
  on the `demo-mcp` service, then `make demo-rebuild`. Use
  `http://localhost:8090/mcp` in the inspector.

This trade-off is documented in IMPL-0002 Resolved OQ #4.

## What works today vs. backlog

- **Default (`make demo-up`) — phases 1a + 1b + 1c (Phase 4 IMPL):** stack
  starts, inspector connects with a static bearer header, eight tools list —
  four GET (list/fetch) plus four PUT/POST (create/update). The generator emits
  a full request pipeline (path-param substitution, JSON body marshaling with
  presence-checked optional fields, status branching) for both shapes.
- **OIDC variant (`make demo-up-oidc`) — phase 2 (Phase 5 IMPL):** the same
  compose stack, plus a hand-rolled `demo-idp` issuer (INV-0001), regenerated
  from `demo/mcpgen-oidc.hcl`. The MCP boundary auth is `oidc` — the inspector
  pastes a JWT minted from `demo-idp/token`, the MCP server validates it against
  `demo-idp/jwks.json`, and tools call `/api/oauth2flow` on the demo API which
  validates the same JWKS. Per Resolved OQ #7, the proxy uses a separate static
  service-account JWT (mint with `make demo-mint-service-token`); end-to-end
  identity propagation is v1.x backlog.

The demo API holds records in memory only — every `make demo-down` or restart of
`demo-api` resets the store to the five seed records. Inspector calls survive a
`demo-mcp` restart (the API container keeps its state).

## Phase 5 (OIDC) walkthrough

The OIDC flow ships behind a separate make target so you don't lose Phase 4's
bearer experience when exploring it.

```bash
# 1. Bring up the OIDC variant. demo-idp comes up alongside the rest;
#    the MCP server validates JWTs against its JWKS endpoint.
make demo-up-oidc

# 2. Mint a service-account JWT for the proxy → /api/oauth2flow leg.
#    Print it, paste into demo/.env as MCP_OAUTH2_SERVICE_TOKEN.
make demo-mint-service-token

# 3. Rebuild so demo-mcp picks up the new env.
make demo-rebuild

# 4. Mint a user JWT for the inspector → MCP leg.
make demo-mint-user-token

# 5. Open the inspector, paste the user JWT into its headers panel
#    as `Authorization: Bearer <token>`, connect to http://demo-mcp:8090/mcp.
open http://localhost:6274
```

Tool calls in the OIDC variant: `list_oauth_records`, `get_oauth_record`,
`create_oauth_record`, `update_oauth_record` — each hits `/api/oauth2flow/...`
on the demo API. Per
[INV-0001](../docs/investigation/0001-oidc-issuer-for-impl-0002-demo.md),
demo-idp is a ~120-LOC Go service that signs RS256 JWTs against an in-memory key
— restart and every previously minted token becomes invalid. Acceptable
trade-off for a demo.

To prove negative paths:

- Wrong audience: `curl "http://localhost:5556/token?aud=wrong"` then paste —
  the inspector should see a 401 from the MCP boundary.
- Expired token: tokens have a 1h lifetime; let one age out and retry —
  same 401.
- Missing service-account JWT: leave `MCP_OAUTH2_SERVICE_TOKEN` empty in `.env`;
  tools list, but each call returns an `upstream 4xx` from `/api/oauth2flow`
  because go-oidc rejects the empty bearer.

## Failure modes you might hit

| Symptom                                                                           | Likely cause                                                                                                                                                                                                      | Fix                                                                                                                                     |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `make demo-up` fails with "demo/.env not found"                                   | Forgot the env copy.                                                                                                                                                                                              | `cp demo/.env.example demo/.env`                                                                                                        |
| `make demo-up` fails with "no such file or directory" near `build/bin/mcp-go-gen` | The Makefile's `build` dep didn't run.                                                                                                                                                                            | `make build && make demo-up`                                                                                                            |
| Port 6274 already in use                                                          | Another inspector / app on that port.                                                                                                                                                                             | `lsof -i :6274` — kill or change the host port in `compose.yaml`.                                                                       |
| Inspector shows "connection refused" on first connect                             | Browser → container path; needs port publishing.                                                                                                                                                                  | Uncomment `ports: ["8090:8090"]` on `demo-mcp` and rebuild.                                                                             |
| Inspector shows 401 / "unauthorized" on the tools list                            | Missing or wrong `Authorization: Bearer` header in the inspector's headers panel (Phase 1b).                                                                                                                      | Copy `MCP_BOUNDARY_TOKEN` value from `demo/.env` into the inspector and reconnect.                                                      |
| Inspector shows "OAuth Authentication Failed: 404 page not found"                 | Custom Header value missing the `Bearer` prefix. The inspector ships the value verbatim, sees a 401, and tries OAuth discovery against the bearer-mode MCP server (which has no OAuth endpoints — hence the 404). | Edit the Authorization header value to read `Bearer <token>` (literal `Bearer`, space, then the token portion of `MCP_BOUNDARY_TOKEN`). |
| Inspector returns 401 even though token "looks right"                             | Pasted the full `.env` value (`<subject>:<token>`) into the inspector. The server-side parser splits on `:` and only accepts the token half.                                                                      | Paste only the part after the `:` — for the default `.env.example`, that's `mcp-boundary-secret-please-change`.                         |
| OIDC variant tools return `upstream 401` on every call                            | `MCP_OAUTH2_SERVICE_TOKEN` empty or invalid (Phase 5).                                                                                                                                                            | `make demo-mint-service-token`, paste into `demo/.env`, `make demo-rebuild`.                                                            |
| OIDC variant inspector tools list fails with 401                                  | The inspector's pasted JWT is expired (1h lifetime), wrong audience, or signed by a previous demo-idp container instance.                                                                                         | `make demo-mint-user-token` and re-paste.                                                                                               |
| Tool calls return `"<tool>: ok"`                                                  | You're on a generator commit before IMPL-0002 phase 1.                                                                                                                                                            | `git pull` and `make demo-rebuild`.                                                                                                     |
| `make ci` fails after editing `demo/api/`                                         | Something in `demo/api/` started failing repo-root tests.                                                                                                                                                         | The demo's separate `go.mod` should keep CI clean — investigate the actual failure.                                                     |

## Testing

The demo API has its own Go test suite that's not part of repo-root
`go test ./...` (separate module, see IMPL-0002 Resolved OQ #3).

```bash
make demo-test
```
