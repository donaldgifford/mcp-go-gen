# mcp-go-gen demo

A self-contained Docker Compose stack that walks `mcp-go-gen` end-to-end:
the **generated MCP server**, a small hand-written **Go HTTP API** for
it to proxy, and the official **MCP Inspector** UI — all on one bridge
network so the inspector can reach the generated server without
`host.docker.internal` gymnastics.

This is the IMPL-0002 deliverable. See:

- `docs/design/0005-docker-compose-demo-and-integration-harness.md` — design.
- `docs/impl/0002-docker-compose-demo-and-integration-harness.md` — phased implementation plan and resolved decisions.

## What's in here

| Path                | What it is                                                   |
| ---                 | ---                                                          |
| `api/`              | The minimal Go HTTP API (separate Go module). Five seed records, three auth trees (`/api/noauth`, `/api/bearer`, `/api/oauth2flow`); phase 1 wires the first two. |
| `mcpgen.hcl`        | The HCL spec the demo MCP server is generated from. Auth = `bearer` (phase 1b) — the inspector pastes a matching `Authorization: Bearer …` before connecting. |
| `mcp-server/`       | Generated each `make demo-up` from `mcpgen.hcl`. **Gitignored** — never edit by hand. |
| `compose.yaml`      | Three services (`demo-api`, `demo-mcp`, `mcp-inspector`) on one bridge. |
| `.env.example`      | Template for `demo/.env` (copy, edit, never commit).         |

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

1. Locate the inspector's headers/auth panel **before** connecting.
   Add a request header:

   ```
   Authorization: Bearer <copy MCP_BOUNDARY_TOKEN value from demo/.env>
   ```

   The MCP server itself reads `MCP_BOUNDARY_TOKEN` from its container
   env (injected by `compose.yaml` from `demo/.env`) and accepts that
   one value. Without a matching header the MCP server rejects the
   connection at its middleware before the tool handler runs.
2. Connect to the demo MCP server. **Verify which URL form works for
   your inspector build** — see [Inspector URL caveat](#inspector-url-caveat)
   below.
3. The tools list should show eight tools — four read + four write:
   - `list_noauth_records`, `get_noauth_record`,
     `list_bearer_records`, `get_bearer_record` (GET).
   - `create_noauth_record`, `update_noauth_record`,
     `create_bearer_record`, `update_bearer_record` (PUT/POST).
4. Call `list_noauth_records` (no input). Expect a JSON list of the
   five seed records.
5. Call `get_bearer_record` with `id=rec-003`. Expect that single
   record returned as JSON.
6. Call `create_noauth_record` with `name="demo"`, `message="hello"`.
   Expect a 201-shaped result with the new record's id (`rec-006`);
   re-call `list_noauth_records` to see the six records.
7. Call `update_bearer_record` with `id=rec-001`, `name="updated"`
   (omit `message` — `RecordPatch` keeps the prior value when a field
   is absent). Expect the patched record back; re-call
   `get_bearer_record` with `id=rec-001` to verify.

To verify the boundary itself: clear the `Authorization` header (or
paste a wrong value) and reconnect — the inspector should surface a
401-shaped error before any tool list call lands. This exercises the
generator's bearer auth middleware (covered by IMPL-0001 phase 4
golden tests).

> **Two perimeters, two tokens.** `MCP_BOUNDARY_TOKEN` gates
> inspector → MCP. `DEMO_BEARER_TOKEN` (mirrored to demo-mcp as
> `MCP_DEMO_API_TOKEN`) gates MCP → `/api/bearer/*`. Either can rotate
> independently. They default to different values in `.env.example`
> for that reason.

```bash
# Tail the stack logs to see what's happening.
make demo-logs

# Tear down (removes containers + networks + volumes).
make demo-down
```

## Inspector URL caveat

The inspector ships as a single container with both UI and MCP-client
code. Whether the MCP request originates from the **container backend**
(Docker DNS resolves `demo-mcp`) or from the **browser** (only sees
`localhost`) depends on the inspector's internals.

- If `http://demo-mcp:8090/mcp` connects on first try → done.
- If it fails with a connection error, the browser is making the
  request directly. Edit `compose.yaml` and uncomment the
  `ports: ["8090:8090"]` block on the `demo-mcp` service, then `make
  demo-rebuild`. Use `http://localhost:8090/mcp` in the inspector.

This trade-off is documented in IMPL-0002 Resolved OQ #4.

## What works today vs. backlog

- **Today (phases 1a + 1b + 1c):** stack starts, inspector connects
  with a bearer header, eight tools list — four GET (list/fetch) plus
  four PUT/POST (create/update). The generator emits a full request
  pipeline (path-param substitution, JSON body marshaling with
  presence-checked optional fields, status branching) for both
  shapes (IMPL-0002 phases 1–4).
- **Phase 2 (next):** OAuth2/OIDC flow with a test issuer service.
  Adds `/api/oauth2flow` to the API and a `demo-idp` service to
  compose. Gated on the issuer-choice INV.

The demo API holds records in memory only — every `make demo-down`
or restart of `demo-api` resets the store to the five seed records.
Inspector calls survive a `demo-mcp` restart (the API container
keeps its state).

## Failure modes you might hit

| Symptom                                              | Likely cause                                            | Fix                                                |
| ---                                                  | ---                                                     | ---                                                |
| `make demo-up` fails with "demo/.env not found"      | Forgot the env copy.                                    | `cp demo/.env.example demo/.env`                   |
| `make demo-up` fails with "no such file or directory" near `build/bin/mcp-go-gen` | The Makefile's `build` dep didn't run. | `make build && make demo-up`                       |
| Port 6274 already in use                             | Another inspector / app on that port.                   | `lsof -i :6274` — kill or change the host port in `compose.yaml`. |
| Inspector shows "connection refused" on first connect | Browser → container path; needs port publishing.        | Uncomment `ports: ["8090:8090"]` on `demo-mcp` and rebuild. |
| Inspector shows 401 / "unauthorized" on the tools list | Missing or wrong `Authorization: Bearer` header in the inspector's headers panel (Phase 1b). | Copy `MCP_BOUNDARY_TOKEN` value from `demo/.env` into the inspector and reconnect. |
| Tool calls return `"<tool>: ok"`                     | You're on a generator commit before IMPL-0002 phase 1.  | `git pull` and `make demo-rebuild`.                |
| `make ci` fails after editing `demo/api/`            | Something in `demo/api/` started failing repo-root tests. | The demo's separate `go.mod` should keep CI clean — investigate the actual failure. |

## Testing

The demo API has its own Go test suite that's not part of repo-root
`go test ./...` (separate module, see IMPL-0002 Resolved OQ #3).

```bash
make demo-test
```
