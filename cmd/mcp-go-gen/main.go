// Package main is the entry point for the mcp-go-gen CLI.
//
// All command handling lives in internal/cli; this file exists only to bind
// the Makefile/goreleaser ldflags (`version`, `commit`) to the CLI runtime
// and to own the single os.Exit call per the Uber Go Style Guide.
package main

import (
	"os"

	"github.com/donaldgifford/mcp-go-gen/internal/cli"
)

// Stamped at build time via -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := cli.Execute(version, commit); err != nil {
		os.Exit(1)
	}
}
