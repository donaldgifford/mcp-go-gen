mcpgen_version = "1"

server {
  name = "bad"
  listener {
    addr = ":7070"
  }
  auth {
    bearer {
      # required attribute tokens_env is missing
      subject_claim = "name"
    }
  }
}
