package scaffold

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModulePath walks upward from startDir looking for a go.mod, parses its
// `module <path>` directive, and returns the declared import path. Used
// by the embed-mode CLI to discover the user module's path before
// generating packages that need to import sibling helpers.
//
// startDir should be the --out directory the generator is writing into.
// The walk stops at the filesystem root; if no go.mod is found an
// actionable error is returned.
func ModulePath(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", startDir, err)
	}
	for {
		candidate := filepath.Join(dir, "go.mod")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return readModuleDirective(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s; embed mode requires an existing Go module at or above --out", startDir)
		}
		dir = parent
	}
}

// readModuleDirective scans a go.mod file for its `module ...` line. Only
// the first match is honored — go.mod enforces a single directive anyway.
func readModuleDirective(path string) (module string, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		return "", fmt.Errorf("open %s: %w", path, openErr)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			rest = strings.TrimSpace(rest)
			rest = strings.Trim(rest, `"`)
			if rest == "" {
				return "", fmt.Errorf("%s: empty module directive", path)
			}
			return rest, nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", fmt.Errorf("scan %s: %w", path, scanErr)
	}
	return "", fmt.Errorf("%s: no module directive found", path)
}
