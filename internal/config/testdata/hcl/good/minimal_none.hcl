mcpgen_version = "1"

server {
  name = "devloop"

  listener {
    addr          = ":7070"
    endpoint_path = "/mcp"
  }

  auth {
    none {}
  }
}

tool "ping" {
  description = "Trivial stub for local testing."

  input {
    field "message" {
      type     = "string"
      required = true
    }
  }
}
