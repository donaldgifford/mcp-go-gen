mcpgen_version = "1"

server {
  name = "oidc-mcp"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  auth {
    oidc {
      issuer          = "https://auth.internal/realms/main"
      jwks_url        = "https://auth.internal/realms/main/protocol/openid-connect/certs"
      audience        = "oidc-mcp"
      required_scopes = ["mcp:read"]
      subject_claim   = "sub"
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
