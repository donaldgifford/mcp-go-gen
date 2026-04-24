mcpgen_version = "1"

server {
  name = "embed-test"

  listener {
    addr = ":7070"
  }

  auth {
    none {}
  }
}

embed {
  target_main = "cmd/svc/main.go"
}

tool "list_deliveries" {
  description = "List recent webhook deliveries."
  input {
    field "limit" {
      type        = "number"
      description = "Max deliveries to return."
    }
  }
}
