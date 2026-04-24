package gen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Writer accepts rendered files and commits them as a unit. Implementations
// must buffer writes internally so that a partial render does not leave the
// output directory in a half-written state — Commit is the only point at
// which bytes reach disk (or stdout, etc.).
type Writer interface {
	// Write records the content planned for path. Implementations should
	// accept calls in any order and dedupe by path.
	Write(path string, content []byte) error
	// Commit finalizes the batched writes. After Commit, the Writer is done.
	Commit() error
}

// FSWriter commits rendered files beneath BaseDir. It enforces the --force
// semantics from IMPL-0001 Phase 3: by default the directory must not exist
// or must be empty; with Force true, files are overwritten.
type FSWriter struct {
	BaseDir string
	Force   bool

	pending map[string][]byte
}

// NewFSWriter constructs an FSWriter rooted at dir.
func NewFSWriter(dir string, force bool) *FSWriter {
	return &FSWriter{BaseDir: dir, Force: force, pending: make(map[string][]byte)}
}

// Write buffers the file until Commit.
func (w *FSWriter) Write(path string, content []byte) error {
	// Defensive copy so callers can reuse their buffers.
	buf := make([]byte, len(content))
	copy(buf, content)
	w.pending[path] = buf
	return nil
}

// Commit creates BaseDir if absent, validates emptiness, and writes every
// buffered file. Intermediate directories are created with 0o750 and files
// with 0o644.
func (w *FSWriter) Commit() error {
	if err := w.prepareDir(); err != nil {
		return err
	}

	// Sort for deterministic output in --verbose logs and test snapshots.
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		full := filepath.Join(w.BaseDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, w.pending[rel], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	return nil
}

func (w *FSWriter) prepareDir() error {
	info, err := os.Stat(w.BaseDir)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(w.BaseDir, 0o750)
	case err != nil:
		return fmt.Errorf("stat %s: %w", w.BaseDir, err)
	case !info.IsDir():
		return fmt.Errorf("%s exists but is not a directory", w.BaseDir)
	}

	if w.Force {
		return nil
	}
	entries, err := os.ReadDir(w.BaseDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", w.BaseDir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty; pass --force to overwrite", w.BaseDir)
	}
	return nil
}

// DryRunWriter prints a summary of planned writes to its underlying
// io.Writer without touching disk. Used by `generate --dry-run`.
type DryRunWriter struct {
	Out     io.Writer
	pending map[string]int // path → byte count
}

// NewDryRunWriter constructs a DryRunWriter that writes to out.
func NewDryRunWriter(out io.Writer) *DryRunWriter {
	return &DryRunWriter{Out: out, pending: make(map[string]int)}
}

// Write records the file for the dry-run summary.
func (w *DryRunWriter) Write(path string, content []byte) error {
	w.pending[path] = len(content)
	return nil
}

// Commit prints one line per planned file: `<path> (<bytes> bytes)`.
// Deterministic ordering makes the output diffable across runs.
func (w *DryRunWriter) Commit() error {
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if _, err := fmt.Fprintf(w.Out, "%s (%d bytes)\n", p, w.pending[p]); err != nil {
			return fmt.Errorf("dry-run write: %w", err)
		}
	}
	return nil
}
