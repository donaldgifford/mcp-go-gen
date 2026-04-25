// Package gen renders IR specs into Go source files via text/template and
// go/format. It owns the embedded template tree at internal/gen/templates/.
//
// The rendering pipeline lands in Phase 3 of IMPL-0001.
package gen
