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
| `mcpgen.hcl`        | The HCL spec the demo MCP server is generated from. Auth = `none` in phase 1a. |
| `mcp-server/`       | Generated each `make demo-up` from `mcpgen.hcl`. **Gitignored** — never edit by hand. |
| `compose.yaml`      | Three services (`demo-api`, `demo-mcp`, `mcp-inspector`) on one bridge. |
| `.env.example`      | Template for `demo/.env` (copy, edit, never commit).         |

## Quickstart

```bash
# 1. Copy the env template and edit if you want a non-default token.
cp demo/.env.example demo/.env

# 2. Bring it up. Generates the MCP server tree, builds all three
#    images, starts the stack detached.
make demo-up

# 3. Open the inspector UI.
open http://localhost:6274
```

In the inspector UI:

1. Connect to the demo MCP server. **Verify which URL form works for
   your inspector build** — see [Inspector URL caveat](#inspector-url-caveat)
   below.
2. The tools list should show four tools:
   `list_noauth_records`, `get_noauth_record`,
   `list_bearer_records`, `get_bearer_record`.
3. Call `list_noauth_records` (no input). Expect a JSON list of the
   five seed records.
4. Call `get_bearer_record` with `id=rec-003`. Expect that single
   record returned as JSON.

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

- **Today (phase 1a):** stack starts, inspector connects, four GET
  tools call the API and return real JSON record data via the
  generator's GET-proxy template (IMPL-0002 phase 1).
- **Phase 1b (next):** swap MCP boundary auth from `none` to `bearer`
  in `mcpgen.hcl`. The inspector picks up an extra step: paste an
  `Authorization: Bearer …` header in its UI before connecting.
- **Phase 1c:** POST/PUT tools added to `mcpgen.hcl` once generator
  write-mutation support lands.
- **Phase 2:** OAuth2/OIDC flow with a test issuer service. Adds
  `/api/oauth2flow` to the API and a `demo-idp` service to compose.

## Failure modes you might hit

| Symptom                                              | Likely cause                                            | Fix                                                |
| ---                                                  | ---                                                     | ---                                                |
| `make demo-up` fails with "demo/.env not found"      | Forgot the env copy.                                    | `cp demo/.env.example demo/.env`                   |
| `make demo-up` fails with "no such file or directory" near `build/bin/mcp-go-gen` | The Makefile's `build` dep didn't run. | `make build && make demo-up`                       |
| Port 6274 already in use                             | Another inspector / app on that port.                   | `lsof -i :6274` — kill or change the host port in `compose.yaml`. |
| Inspector shows "connection refused" on first connect | Browser → container path; needs port publishing.        | Uncomment `ports: ["8090:8090"]` on `demo-mcp` and rebuild. |
| Tool calls return `"<tool>: ok"`                     | You're on a generator commit before IMPL-0002 phase 1.  | `git pull` and `make demo-rebuild`.                |
| `make ci` fails after editing `demo/api/`            | Something in `demo/api/` started failing repo-root tests. | The demo's separate `go.mod` should keep CI clean — investigate the actual failure. |

## Testing

The demo API has its own Go test suite that's not part of repo-root
`go test ./...` (separate module, see IMPL-0002 Resolved OQ #3).

```bash
make demo-test
```
