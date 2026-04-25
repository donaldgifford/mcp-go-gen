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
    spec = "../../openapi/nested_object.yaml"
  }
}

tool "create_thing" {
  openapi_operation = "createThing"
}
