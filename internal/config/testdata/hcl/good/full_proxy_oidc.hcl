mcpgen_version = "1"

server {
  name    = "rfc-api-mcp"
  version = "1.0.0"

  listener {
    addr          = ":7070"
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
      addr    = ":9090"
    }
    tracing {
      enabled      = true
      service_name = "rfc-api-mcp"
      sample_ratio = 1.0
      exporter     = "otlp_http"
      endpoint     = "http://localhost:4318"
    }
  }

  auth {
    oidc {
      issuer          = "https://auth.internal/realms/main"
      jwks_url        = "https://auth.internal/realms/main/protocol/openid-connect/certs"
      audience        = "mcp-rfc-api"
      required_scopes = ["mcp:read"]
      subject_claim   = "sub"
    }
  }
}

proxy {
  base_url = "https://api.example.com"

  auth {
    bearer {
      token_env = "BACKEND_API_TOKEN"
    }
  }

  timeouts {
    dial            = "5s"
    total           = "30s"
    idle_connection = "90s"
  }

  retry {
    max_attempts    = 3
    retry_on_status = [502, 503, 504]
    base_delay      = "200ms"
  }
}

tool "get_rfc" {
  description = "Fetch an RFC by its identifier."

  input {
    field "id" {
      type        = "string"
      required    = true
      description = "RFC identifier, e.g. RFC-0042."
    }
  }

  backend "http" {
    method = "GET"
    path   = "/rfcs/{id}"

    path_param "id" {
      from = "id"
    }

    response {
      type             = "json"
      content_template = "RFC {{.id}}: {{.title}}"
    }

    on_error {
      not_found = "RFC %s not found"
    }
  }
}
