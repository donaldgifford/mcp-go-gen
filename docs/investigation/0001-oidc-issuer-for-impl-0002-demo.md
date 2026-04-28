---
id: INV-0001
title: "OIDC issuer for IMPL-0002 demo"
status: Concluded
author: Donald Gifford
created: 2026-04-27
---
<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0001: OIDC issuer for IMPL-0002 demo

**Status:** Concluded
**Author:** Donald Gifford
**Date:** 2026-04-27

<!--toc:start-->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Findings](#findings)
  - [Option A — dex](#option-a--dex)
  - [Option B — Keycloak](#option-b--keycloak)
  - [Option C — hand-rolled JWKS issuer](#option-c--hand-rolled-jwks-issuer)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

What's the right test issuer for IMPL-0002 Phase 5's `demo-idp` Compose service? The criteria from IMPL-0002 are: bootstrap complexity, image size, ability to declare issuer + audience + signing key in a single config file, license, and container-image trust.

## Hypothesis

A hand-rolled Go service that signs RS256 JWTs from a fixed test key and exposes the two endpoints `go-oidc/v3` requires (`/.well-known/openid-configuration` and `/jwks.json`) wins on every criterion that matters for a *demo*: tiny image, instant startup, single-file config, no realm/client setup ceremony. The off-the-shelf options (dex, Keycloak) are designed for production-ish multi-tenant identity flows and bring config surface area and image weight that hurt the demo's "one command, three minutes" promise more than they help.

## Context

**Triggered by:** IMPL-0002 Phase 5, which is gated on this choice.

The demo is meant to walk a generator user through the four mcpgen auth schemes one phase at a time. Phase 5 swaps the MCP boundary from `bearer` (Phase 3) to `oidc` and adds a backend tree (`/api/oauth2flow`) that validates the same JWT shape on the upstream side.

The issuer's job in this demo is *narrow*: issue an RS256 JWT with a configurable `iss`, `aud`, `sub`, `exp` and serve a JWKS so both the MCP server and the demo API can verify it. It does not need user federation, refresh-token rotation, admin UI, or any other production identity feature.

## Approach

1. List the criteria from IMPL-0002 §"Investigation prereq" verbatim and score each candidate.
2. For each candidate, capture: image size on `linux/amd64`, default container startup time on the laptop, single-step config (yes/no), license, and lines of YAML/JSON to declare one issuer + one client + one signing key.
3. Pick the option with the lowest aggregate friction for the demo's narrow needs. Specifically reject candidates whose smallest-viable config is more than ~30 lines or whose container needs admin clicks before it issues tokens.

## Findings

### Option A — dex

- **Image:** `ghcr.io/dexidp/dex:v2.41.1`, ~120MB compressed.
- **Startup:** ~1s once config is mounted.
- **Config:** Single YAML file with `issuer`, `storage`, `staticClients`, `staticPasswords`, `oauth2.skipApprovalScreen`. ~25 lines minimum for client-credentials-style flow.
- **Token endpoint:** Fully OIDC-compliant — discovery + JWKS + token + userinfo all there. Demo only needs the first two.
- **License:** Apache-2.0.
- **Container trust:** Official image from CNCF graduated project, regular releases, signed images.
- **Friction for demo:** Static-password connectors require either an interactive auth flow or a separate `/token` POST with a hashed password — neither is a "one curl gets a JWT" story for inspector users. Client-credentials flow needs a connector configuration that's not trivial.

### Option B — Keycloak

- **Image:** `quay.io/keycloak/keycloak:26.0`, ~700MB.
- **Startup:** ~30–60s on first run (DB schema migration), ~10s subsequent.
- **Config:** Realm import JSON or env-var-driven realm setup. Realm JSON is hundreds of lines.
- **Token endpoint:** Same compliance as dex; over-featured for demo.
- **License:** Apache-2.0.
- **Container trust:** Official Red Hat / quay.io image.
- **Friction for demo:** Image weight and startup time alone push past the IMPL-0002 §"all healthy within 90 seconds" target. Realm bootstrap requires either an admin login + clicks or a multi-hundred-line realm JSON.

### Option C — hand-rolled JWKS issuer

- **Implementation:** ~120 LOC of Go: an `http.ServeMux` with two routes (`/.well-known/openid-configuration` returning the discovery doc; `/jwks.json` returning the public key) plus a `/token` endpoint that takes a `subject` query param, signs an RS256 JWT with hard-coded claims (`iss`, `aud`, `sub`, `exp = now+1h`), and returns it as `{ "access_token": "<jwt>" }`. Signing key generated once at startup with `rsa.GenerateKey(rand.Reader, 2048)` and held in memory. Public key serialized as JWK on demand.
- **Image:** Multi-stage build → `gcr.io/distroless/static:nonroot`, ~10MB.
- **Startup:** Sub-second — no DB, no migration, no config parsing, just keygen + listen.
- **Config:** Two env vars: `IDP_ISSUER` (e.g. `http://demo-idp:5556`) and `IDP_AUDIENCE` (e.g. `mcp-demo-api`).
- **Token endpoint:** `GET /token?sub=alice` returns a JWT immediately. Inspector users `curl` it once, paste the result.
- **License:** Internal demo code, Apache-2.0 with the rest of the repo.
- **Container trust:** Built from this repo's own source, no third-party runtime.
- **Friction for demo:** None of the production-grade issuer features. Cannot rotate keys, has no client registration, no PKCE, no scope enforcement beyond what we hard-code. *All of these are non-goals for the demo.*

## Conclusion

**Answer:** Option C — hand-rolled JWKS issuer.

Aggregate friction is dramatically lower than either off-the-shelf option, and every "lost" feature is something IMPL-0002 explicitly does not need. dex's main pull is "real OIDC compliance," which we already get from the *clients* (the `oidc` and `oidc_dynamic` mcpgen auth schemes use `coreos/go-oidc` for verification — that's the side of the wire that needs to be standards-compliant). The *server* side just has to produce a valid JWT and JWKS; correctness is verified by the unmodified `go-oidc` verifier on the demo MCP and demo API.

The 90-second healthy-services target in IMPL-0002 success criteria is met easily; with Keycloak it would be the dominant constraint.

## Recommendation

1. Implement `demo/idp/` as a small Go service per the Option C sketch above. Include a `/token` endpoint so users can `curl` for a JWT before pasting into the inspector — this keeps the user-visible path identical to the bearer-token paste flow from Phase 3.
2. Use `coreos/go-oidc/v3` on the demo API side (`oidcAuth(issuer, audience)` middleware) for the same verification path the generator's `oidc` auth template uses.
3. Update IMPL-0002 Open Question #1 (issuer choice) to "Resolved" pointing at this INV.
4. Re-evaluate when the demo grows beyond a learning aid — if a real PKCE / authorization-code flow is added later, swap to dex at that point. The hand-rolled service is small enough to delete in one commit.

## References

- IMPL-0002 §"Phase 5: OAuth2/OIDC flow" — gating context.
- DESIGN-0005 §"Phase 2 — OAuth2 flow" — original design target.
- `internal/gen/templates/internal/mcpauth/auth_oidc*.go.tmpl` — the verifier code path the issuer must satisfy.
- `github.com/coreos/go-oidc/v3` — JWKS verifier; already in the generator's transitive deps.
