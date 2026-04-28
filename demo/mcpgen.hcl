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

# Phase 1c: write-mutation tools (IMPL-0002 Phase 4). PUT creates, POST
# updates by id. The demo API decodes JSON bodies into RecordPatch (for
# updates) and CreateInput (for creates); the generator's body_param
# blocks below produce matching JSON shapes.

tool "create_noauth_record" {
  description = "Create a new record in the no-auth tree."

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
    path   = "/api/noauth"

    body_param "name" {
      from = "name"
    }
    body_param "message" {
      from = "message"
    }
  }
}

tool "update_noauth_record" {
  description = "Update an existing record in the no-auth tree by id. Both fields are optional — omitted fields keep their current value."

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
    path   = "/api/noauth/{id}"

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

tool "create_bearer_record" {
  description = "Create a new record in the bearer-protected tree."

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
    path   = "/api/bearer"

    body_param "name" {
      from = "name"
    }
    body_param "message" {
      from = "message"
    }
  }
}

tool "update_bearer_record" {
  description = "Update an existing record in the bearer-protected tree by id. Both fields are optional — omitted fields keep their current value."

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
    path   = "/api/bearer/{id}"

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
