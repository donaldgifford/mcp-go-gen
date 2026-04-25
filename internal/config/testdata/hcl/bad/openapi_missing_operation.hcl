mcpgen_version = "1"

server {
  name = "bad"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  auth {
    none {}
  }
}

proxy {
  base_url = "https://api.example.com"

  openapi {
    spec = "../../openapi/rfc_api.yaml"
  }
}

tool "missing" {
  openapi_operation = "doesNotExist"
}
