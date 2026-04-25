// Package cli implements the mcp-go-gen command-line interface.
//
// The binary entry point in cmd/mcp-go-gen/main.go calls Execute, which
// builds and runs the Cobra command tree: init, validate, generate.
// Each subcommand lives in its own file.
package cli
