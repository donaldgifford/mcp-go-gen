# IMPL-0002 Phase 5 — OIDC variant of the demo MCP. Switches the boundary
# auth from `bearer` (mcpgen.hcl) to `oidc` against the hand-rolled
# demo-idp issuer (INV-0001), and replaces the eight Phase-1c bearer
# tools with four `/api/oauth2flow` tools that exercise the demo API's
# OIDC-validated tree.
#
# Driven by `make demo-up-oidc` which regenerates `demo/mcp-server` from
# this file instead of the bearer-mode `demo/mcpgen.hcl`. Phase 4's
# eight-tool flow stays available via the default `make demo-up` —
# no tool gets clobbered when switching back.

mcpgen_version = "1"

server {
  name    = "demo-mcp"
  version = "0.0.1"

  listener {
    addr          = ":8090"
    endpoint_path = "/mcp"
  }

  observability {
    logging {
      format = "json"
      level  = "info"
    }
    metrics {
      enabled = true
      path    = "/metrics"
    }
    tracing {
      enabled = false
    }
  }

  # Phase 5: MCP boundary auth = OIDC. Inspector pastes a JWT minted
  # from demo-idp's /token endpoint into its headers/auth panel before
  # connecting. The MCP server fetches JWKS from
  # http://demo-idp:5556/jwks.json at startup (via go-oidc) and
  # validates every incoming Authorization: Bearer <jwt> against it.
  auth {
    oidc {
      issuer    = "http://demo-idp:5556"
      jwks_url  = "http://demo-idp:5556/jwks.json"
      audience  = "mcp-demo-api"
    }
  }
}

proxy {
  base_url = "http://demo-api:8080"

  auth {
    bearer {
      # Per IMPL-0002 Resolved OQ #7: the proxy uses a SEPARATE static
      # service-account JWT (not the user's). Mint with
      # `make demo-mint-service-token`, paste into demo/.env, restart.
      # The demo API validates this string against demo-idp's JWKS the
      # same way it validates the inspector's JWT — same issuer, same
      # audience, different sub.
      token_env = "MCP_OAUTH2_SERVICE_TOKEN"
    }
  }

  timeouts {
    total = "5s"
  }
}

tool "list_oauth_records" {
  description = "List records from the OIDC-protected tree."

  backend "http" {
    method = "GET"
    path   = "/api/oauth2flow"
  }
}

tool "get_oauth_record" {
  description = "Fetch one record from the OIDC-protected tree by id."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "Record identifier, e.g. rec-001."
    }
  }

  backend "http" {
    method = "GET"
    path   = "/api/oauth2flow/{id}"

    path_param "id" {
      from = "id"
    }
  }
}

tool "create_oauth_record" {
  description = "Create a new record in the OIDC-protected tree."

  input {
    field "name" {
      type        = "string"
      required    = true
      description = "Display name for the new record."
    }
    field "message" {
      type        = "string"
      required    = true
      description = "Free-text payload."
    }
  }

  backend "http" {
    method = "PUT"
    path   = "/api/oauth2flow"

    body_param "name" {
      from = "name"
    }
    body_param "message" {
      from = "message"
    }
  }
}

tool "update_oauth_record" {
  description = "Update an existing record in the OIDC-protected tree by id. Both fields are optional — omitted fields keep their current value."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "Record identifier to update."
    }
    field "name" {
      type        = "string"
      required    = false
      description = "Optional new display name."
    }
    field "message" {
      type        = "string"
      required    = false
      description = "Optional new message text."
    }
  }

  backend "http" {
    method = "POST"
    path   = "/api/oauth2flow/{id}"

    path_param "id" {
      from = "id"
    }
    body_param "name" {
      from = "name"
    }
    body_param "message" {
      from = "message"
    }
  }
}
