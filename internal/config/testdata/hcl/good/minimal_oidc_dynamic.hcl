mcpgen_version = "1"

server {
  name = "oidc-dyn-mcp"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  auth {
    oidc_dynamic {
      issuer          = "https://auth.internal/realms/main"
      audience        = "oidc-dyn-mcp"
      required_scopes = ["mcp:read"]
      subject_claim   = "sub"
      cache_ttl       = "1h"
    }
  }
}

tool "ping" {
  description = "Trivial stub."

  input {
    field "message" {
      type     = "string"
      required = true
    }
  }
}
