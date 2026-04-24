mcpgen_version = "1"

server {
  name = "bad"
  listener {
    addr = ":7070"
  }
  # no auth block — decoder should reject
}
