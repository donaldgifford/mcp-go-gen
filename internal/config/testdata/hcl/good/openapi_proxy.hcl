mcpgen_version = "1"

server {
  name = "rfc-api-mcp"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  auth {
    bearer {
      tokens_env = "MCP_TOKENS"
    }
  }
}

proxy {
  base_url = "https://api.example.com"

  openapi {
    spec = "../../openapi/rfc_api.yaml"
  }
}

tool "get_rfc" {
  openapi_operation = "getRfcById"
  description       = "Fetch an RFC by id (HCL description wins over OpenAPI)."
}

tool "list_rfcs" {
  openapi_operation = "listRfcs"
}
