package config

import "testing"

// FuzzHCLDecode drives arbitrary bytes through DecodeBytes to make sure the
// decoder always returns diagnostics rather than panicking. Per DESIGN-0004
// §"Testing Strategy" the target runs at least 1 minute in CI; locally it
// runs until the fuzz budget expires.
//
// Seeds come from the good and bad fixtures so the corpus starts from
// plausible HCL rather than noise.
func FuzzHCLDecode(f *testing.F) {
	seeds := []string{
		`mcpgen_version = "1"`,
		``,
		`server { name = "x" }`,
		`mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}
`,
		`mcpgen_version = "1"
server {
  name = "x"
  listener { addr = ":7070" }
  auth {
    bearer {
      tokens_env = "T"
    }
  }
}
tool "t" {
  description = "x"
}
`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(_ *testing.T, data []byte) {
		// DecodeBytes must never panic. We do not assert on the return
		// values — any malformed input is allowed to produce diagnostics.
		_, _ = DecodeBytes(data, "fuzz.hcl")
	})
}
