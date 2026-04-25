// Package dst performs structural edits to existing user Go source via
// github.com/dave/dst. Its single responsibility is finding the
// // mcpgen:hook marker in a target main.go and inserting the generated
// package's Register call idempotently.
//
// Lands in Phase 6 of IMPL-0001.
package dst
