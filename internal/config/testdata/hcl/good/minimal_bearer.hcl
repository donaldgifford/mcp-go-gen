mcpgen_version = "1"

server {
  name = "minimal"

  listener {
    addr = ":7070"
  }

  auth {
    bearer {
      tokens_env = "MCP_TOKENS"
    }
  }
}

tool "echo" {
  description = "Echo a string."
  input {
    field "msg" {
      type = "string"
    }
  }
}
