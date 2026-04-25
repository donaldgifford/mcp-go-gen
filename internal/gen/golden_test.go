package gen_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/config"
	"github.com/donaldgifford/mcp-go-gen/internal/gen"
)

var updateGoldens = flag.Bool("update", false, "regenerate golden-file fixtures")

// TestRender_GoldenFiles pins the shape of the generated output for a
// known fixture. The test renders minimal_bearer.hcl into a tempdir and
// snapshots the list of files + a subset of each file's first lines so
// a template change that silently drops a file shows up as a diff here.
//
// Full content comparison of every template produces large goldens that
// churn with every dependency bump; this lighter pin catches layout
// regressions without the maintenance cost.
//
// Run `go test -update ./internal/gen/...` to refresh after intentional
// template changes.
func TestRender_GoldenFiles(t *testing.T) {
	fixture := filepath.Join("..", "config", "testdata", "hcl", "good", "minimal_bearer.hcl")
	cfg, diags := config.Decode(fixture)
	if diags.HasErrors() {
		t.Fatalf("decode: %s", diags.Error())
	}
	spec, err := config.ToIR(cfg)
	if err != nil {
		t.Fatalf("ToIR: %v", err)
	}

	out := t.TempDir()
	if err := gen.Render(spec, gen.NewFSWriter(out, false)); err != nil {
		t.Fatalf("Render: %v", err)
	}

	var listing []string
	walkErr := filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(out, p)
		if relErr != nil {
			return relErr
		}
		listing = append(listing, rel)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	sort.Strings(listing)
	got := strings.Join(listing, "\n") + "\n"

	goldenPath := filepath.Join("testdata", "golden", "minimal_bearer.files.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", goldenPath, err)
	}
	if string(want) != got {
		t.Errorf("file listing drifted from golden %s.\nwant:\n%s\ngot:\n%s",
			goldenPath, string(want), got)
	}
}
