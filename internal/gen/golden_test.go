package gen_test

import (
	"bytes"
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

// TestRender_ProxyGetTools pins the contents of internal/mcpserver/tools.go
// for a proxy spec with a GET tool, so a regression in the GET-proxy
// template logic (path-param substitution, header injection, response
// branching) shows up here as a content diff. Complements
// TestRender_GoldenFiles which only pins the file listing.
//
// Run `go test -update ./internal/gen/...` to refresh after intentional
// template changes.
func TestRender_ProxyGetTools(t *testing.T) {
	fixture := filepath.Join("..", "config", "testdata", "hcl", "good", "full_proxy_oidc.hcl")
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

	got, err := os.ReadFile(filepath.Join(out, "internal", "mcpserver", "tools.go"))
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "proxy_get_tools.go.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", goldenPath, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("tools.go drifted from golden %s.\n--- want ---\n%s\n--- got ---\n%s",
			goldenPath, string(want), string(got))
	}
}

// TestRender_ProxyGetTools_ShapeAssertions complements the golden by checking
// that specific Go constructs essential to the GET-proxy contract are
// present in the rendered tools.go. The golden file pins the exact byte
// layout; these substring checks catch the case where someone updates the
// golden but accidentally drops a critical line. Every assertion here
// represents a property the demo (Phase 2) and downstream consumers
// silently depend on.
//
// Runtime exercise (RoundTripper-stubbed happy/4xx/network paths) is
// deferred to the Phase 2 demo's manual end-to-end verification — see
// Resolved OQ #1 in IMPL-0002 for the rationale.
func TestRender_ProxyGetTools_ShapeAssertions(t *testing.T) {
	fixture := filepath.Join("..", "config", "testdata", "hcl", "good", "full_proxy_oidc.hcl")
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

	body, err := os.ReadFile(filepath.Join(out, "internal", "mcpserver", "tools.go"))
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	src := string(body)

	mustContain := []struct {
		name, needle string
	}{
		{"path-param map allocation", `pathParams := map[string]string{`},
		{"path-param entry for id", `"id": id`},
		{"path-substitution helper call", `t.Backend.SubstitutePath("/rfcs/{id}", pathParams)`},
		{"backend base URL prefixing", `t.Backend.BaseURL + rawPath`},
		{"http GET request construction", `http.NewRequestWithContext(ctx, "GET", u.String(), nil)`},
		{"bearer token attachment", `httpReq.Header.Set("Authorization", "Bearer "+t.Backend.Token)`},
		{"backend client invocation", `t.Backend.Client.Do(httpReq)`},
		{"response body cap", `io.LimitReader(resp.Body, 1<<20)`},
		{"2xx success outcome", `t.recordOutcome("get_rfc", subj.Name, "success", start)`},
		{"4xx outcome label", `"upstream_4xx"`},
		{"5xx outcome label", `"upstream_5xx"`},
		{"network error outcome", `"network_error"`},
		{"structured-or-text result helper", `return toolResultFromBody(body), nil`},
		{"toolResultFromBody definition uses NewToolResultStructured", `mcp.NewToolResultStructured(parsed, string(body))`},
		{"toolResultFromBody fallback to text", `return mcp.NewToolResultText(string(body))`},
		{"upstream error result type", `mcp.NewToolResultError(fmt.Sprintf("upstream %d:`},
	}

	for _, c := range mustContain {
		if !strings.Contains(src, c.needle) {
			t.Errorf("rendered tools.go missing %s; wanted to find %q", c.name, c.needle)
		}
	}
}

// TestRender_ProxyPostTools pins the shape of the rendered tools.go for a
// proxy spec with a POST tool that declares body_param blocks. Catches the
// case where the body-marshaling branch in tools.go.tmpl drops a critical
// piece (json.Marshal call, args-presence check, Content-Type header,
// non-GET method literal).
//
// Driven by an inline HCL fixture rather than a fresh testdata file so the
// fixture's intent is obvious next to the assertions.
func TestRender_ProxyPostTools(t *testing.T) {
	src := `
mcpgen_version = "1"

server {
  name = "test_post"
  listener { addr = ":7070" }
  auth {
    none {}
  }
}

proxy {
  base_url = "http://127.0.0.1:9999"
}

tool "update_thing" {
  description = "Update a thing."
  input {
    field "id" {
      type     = "string"
      required = true
    }
    field "name" {
      type     = "string"
      required = false
    }
  }
  backend "http" {
    method = "POST"
    path   = "/things/{id}"
    path_param "id" {
      from = "id"
    }
    body_param "name" {
      from = "name"
    }
  }
}
`
	cfg, diags := config.DecodeBytes([]byte(src), "inline.hcl")
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
	body, err := os.ReadFile(filepath.Join(out, "internal", "mcpserver", "tools.go"))
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	rendered := string(body)

	mustContain := []struct {
		name, needle string
	}{
		{"bytes import", `"bytes"`},
		{"encoding/json import", `"encoding/json"`},
		{"required input via RequireString", `req.RequireString("id")`},
		{"optional input via GetString", `req.GetString("name", "")`},
		{"args map for body presence", `args := req.GetArguments()`},
		{"body map allocation", `bodyMap := make(map[string]any, 1)`},
		{"presence-checked body insertion", `if v, ok := args["name"]; ok {`},
		{"json marshal", `json.Marshal(bodyMap)`},
		{"POST method literal", `http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(bodyBytes))`},
		{"json content type", `httpReq.Header.Set("Content-Type", "application/json")`},
		{"path helper for path-param tools", `t.Backend.SubstitutePath("/things/{id}", pathParams)`},
	}
	for _, c := range mustContain {
		if !strings.Contains(rendered, c.needle) {
			t.Errorf("rendered tools.go missing %s; wanted %q", c.name, c.needle)
		}
	}
}

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
