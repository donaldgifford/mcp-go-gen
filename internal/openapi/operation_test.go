package openapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donaldgifford/mcp-go-gen/internal/openapi"
)

func TestLoad_Good(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "config", "testdata", "openapi", "rfc_api.yaml")
	doc, err := openapi.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc == nil {
		t.Fatal("Load returned nil doc")
	}
}

func TestOperation_ResolvesByID(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "config", "testdata", "openapi", "rfc_api.yaml")
	doc, err := openapi.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	op, err := doc.Operation("getRfcById")
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if op.Method != "GET" || op.Path != "/rfcs/{id}" {
		t.Errorf("getRfcById: got %s %s, want GET /rfcs/{id}", op.Method, op.Path)
	}
	if len(op.Parameters) != 2 {
		t.Fatalf("want 2 parameters, got %d", len(op.Parameters))
	}

	var idParam openapi.Parameter
	for _, p := range op.Parameters {
		if p.Name == "id" {
			idParam = p
			break
		}
	}
	if idParam.In != "path" || !idParam.Required || idParam.Schema.Kind != openapi.SchemaString {
		t.Errorf("id param = %+v, want path/required/string", idParam)
	}
}

func TestOperation_EnumSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "config", "testdata", "openapi", "rfc_api.yaml")
	doc, _ := openapi.Load(path)

	op, err := doc.Operation("listRfcs")
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}

	var statusParam openapi.Parameter
	for _, p := range op.Parameters {
		if p.Name == "status" {
			statusParam = p
			break
		}
	}
	if statusParam.Schema.Kind != openapi.SchemaEnum {
		t.Errorf("status.Kind = %d, want SchemaEnum", statusParam.Schema.Kind)
	}
	if len(statusParam.Schema.Enum) != 3 {
		t.Errorf("enum len = %d, want 3", len(statusParam.Schema.Enum))
	}
}

func TestOperation_ArrayOfStrings(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "config", "testdata", "openapi", "rfc_api.yaml")
	doc, _ := openapi.Load(path)

	op, _ := doc.Operation("listRfcs")
	var tagsParam openapi.Parameter
	for _, p := range op.Parameters {
		if p.Name == "tags" {
			tagsParam = p
			break
		}
	}
	if tagsParam.Schema.Kind != openapi.SchemaArrayString {
		t.Errorf("tags.Kind = %d, want SchemaArrayString", tagsParam.Schema.Kind)
	}
}

func TestOperation_NotFound(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "config", "testdata", "openapi", "rfc_api.yaml")
	doc, _ := openapi.Load(path)

	_, err := doc.Operation("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing operation")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want contains 'not found'", err)
	}
}

func TestLoad_RejectsSwagger(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "swagger.yaml")
	swagger := []byte("swagger: \"2.0\"\ninfo:\n  title: legacy\n  version: 1\npaths: {}\n")
	if err := os.WriteFile(path, swagger, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := openapi.Load(path)
	if err == nil {
		t.Fatal("expected error loading Swagger 2.0")
	}
	if !strings.Contains(err.Error(), "OpenAPI 2.0") {
		t.Errorf("err = %v, want mentions OpenAPI 2.0", err)
	}
}

func TestLoad_RejectsRemoteRefs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "remote_ref.yaml")
	body := []byte(`openapi: 3.0.3
info:
  title: test
  version: 1
paths:
  /x:
    get:
      operationId: getX
      parameters:
        - $ref: "https://example.com/refs.yaml#/components/parameters/X"
      responses:
        '200':
          description: ok
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := openapi.Load(path)
	if err == nil {
		t.Fatal("expected error loading doc with remote $ref")
	}
	if !strings.Contains(err.Error(), "remote $ref") {
		t.Errorf("err = %v, want mentions remote $ref", err)
	}
}
