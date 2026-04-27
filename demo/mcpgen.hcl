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

  # Phase 1b: MCP boundary auth is `bearer`. The inspector must paste an
  # Authorization: Bearer <MCP_BOUNDARY_TOKEN> header into its UI before
  # connecting. Phase 2 swaps this for `oidc` against a test issuer.
  auth {
    bearer {
      tokens_env = "MCP_BOUNDARY_TOKEN"
    }
  }
}

proxy {
  base_url = "http://demo-api:8080"

  auth {
    bearer {
      token_env = "MCP_DEMO_API_TOKEN"
    }
  }

  timeouts {
    total = "5s"
  }
}

tool "list_noauth_records" {
  description = "List all records from the no-auth API tree."

  backend "http" {
    method = "GET"
    path   = "/api/noauth"
  }
}

tool "get_noauth_record" {
  description = "Fetch one record from the no-auth tree by id."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "Record identifier, e.g. rec-001."
    }
  }

  backend "http" {
    method = "GET"
    path   = "/api/noauth/{id}"

    path_param "id" {
      from = "id"
    }
  }
}

tool "list_bearer_records" {
  description = "List all records from the bearer-protected tree."

  backend "http" {
    method = "GET"
    path   = "/api/bearer"
  }
}

tool "get_bearer_record" {
  description = "Fetch one record from the bearer-protected tree by id."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "Record identifier, e.g. rec-001."
    }
  }

  backend "http" {
    method = "GET"
    path   = "/api/bearer/{id}"

    path_param "id" {
      from = "id"
    }
  }
}
