package gen

// Plan describes one file the renderer will write. Each Plan names the
// destination path relative to --out, the embedded template to evaluate, and
// the root data passed to that template. The GoFormat flag asks the
// renderer to run go/format.Source on the output — set for generated .go
// files, never for YAML/HCL/Markdown.
//
// The Plan struct is stable across phases: later phases add more Plans for
// more templates without changing this type.
type Plan struct {
	Path     string
	Template string
	Data     any
	GoFormat bool
}
