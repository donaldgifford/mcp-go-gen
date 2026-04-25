package openapi

import (
	"fmt"
	"strings"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	yaml "go.yaml.in/yaml/v4"
)

// Operation is the resolved shape of one OpenAPI operation, flattened into
// the fields mcpgen's code generator consumes. Only the pieces the
// generator actually uses are populated — response-body selection lands
// in v1.x.
type Operation struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string
	Parameters  []Parameter
}

// Parameter is one path/query/header parameter with its resolved primitive
// type. Nested-object parameters surface as an error at resolution time;
// downstream code never sees them.
type Parameter struct {
	Name        string
	In          string // "path" | "query" | "header"
	Required    bool
	Description string
	Schema      Schema
}

// SchemaKind enumerates the primitive OpenAPI types mcpgen v1 accepts.
type SchemaKind int

// SchemaKind values. The deliberate absence of an "object" variant is how
// "nested objects are out of scope for v1" is enforced: the resolver never
// produces that value.
const (
	SchemaString SchemaKind = iota + 1
	SchemaNumber
	SchemaBoolean
	SchemaEnum
	SchemaArrayString
	SchemaArrayNumber
	SchemaArrayBoolean
)

// Schema holds the primitive resolution of an OpenAPI schema node.
type Schema struct {
	Kind SchemaKind
	Enum []string
}

// Operation resolves an operation by its operationId across every path and
// method in the document. The first match wins; OpenAPI already enforces
// uniqueness of operationIds within a document.
func (d *Doc) Operation(id string) (*Operation, error) {
	if d.model == nil || d.model.Paths == nil {
		return nil, fmt.Errorf("document has no paths")
	}

	for p, item := range d.model.Paths.PathItems.FromOldest() {
		for method, op := range operationsOf(item) {
			if op == nil || op.OperationId != id {
				continue
			}
			resolved, err := resolveOperation(p, method, op)
			if err != nil {
				return nil, fmt.Errorf("operation %q: %w", id, err)
			}
			return resolved, nil
		}
	}
	return nil, fmt.Errorf("operation %q not found in document", id)
}

// operationsOf returns a method→operation map for a single PathItem so the
// caller can scan for a matching operationId without repeating the switch.
func operationsOf(item *v3.PathItem) map[string]*v3.Operation {
	return map[string]*v3.Operation{
		"GET":     item.Get,
		"POST":    item.Post,
		"PUT":     item.Put,
		"PATCH":   item.Patch,
		"DELETE":  item.Delete,
		"HEAD":    item.Head,
		"OPTIONS": item.Options,
		"TRACE":   item.Trace,
	}
}

func resolveOperation(path, method string, op *v3.Operation) (*Operation, error) {
	resolved := &Operation{
		ID:          op.OperationId,
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
	}

	for _, p := range op.Parameters {
		if p == nil {
			continue
		}
		param, err := resolveParameter(p)
		if err != nil {
			return nil, err
		}
		resolved.Parameters = append(resolved.Parameters, param)
	}
	return resolved, nil
}

func resolveParameter(p *v3.Parameter) (Parameter, error) {
	required := false
	if p.Required != nil {
		required = *p.Required
	}

	param := Parameter{
		Name:        p.Name,
		In:          strings.ToLower(p.In),
		Required:    required,
		Description: p.Description,
	}
	if p.Schema == nil {
		return Parameter{}, fmt.Errorf("parameter %q has no schema", p.Name)
	}
	schema, err := resolveSchema(p.Name, p.Schema.Schema())
	if err != nil {
		return Parameter{}, err
	}
	param.Schema = schema
	return param, nil
}

func resolveSchema(paramName string, s *base.Schema) (Schema, error) {
	if s == nil {
		return Schema{}, fmt.Errorf("parameter %q: nil schema", paramName)
	}

	t := primaryType(s.Type)
	switch t {
	case "object":
		return Schema{}, fmt.Errorf("parameter %q uses a nested object; mcpgen v1 does not support nested inputs", paramName)
	case "array":
		return resolveArraySchema(paramName, s)
	case "string":
		if len(s.Enum) > 0 {
			return Schema{Kind: SchemaEnum, Enum: enumStrings(s.Enum)}, nil
		}
		return Schema{Kind: SchemaString}, nil
	case "integer", "number":
		return Schema{Kind: SchemaNumber}, nil
	case "boolean":
		return Schema{Kind: SchemaBoolean}, nil
	case "":
		return Schema{}, fmt.Errorf("parameter %q has no type (schema may require $ref resolution mcpgen v1 does not support)", paramName)
	default:
		return Schema{}, fmt.Errorf("parameter %q uses unsupported type %q", paramName, t)
	}
}

func resolveArraySchema(paramName string, s *base.Schema) (Schema, error) {
	if s.Items == nil || s.Items.A == nil {
		return Schema{}, fmt.Errorf("parameter %q: array schema missing items", paramName)
	}
	inner := s.Items.A.Schema()
	if inner == nil {
		return Schema{}, fmt.Errorf("parameter %q: array items has no schema", paramName)
	}
	switch primaryType(inner.Type) {
	case "string":
		return Schema{Kind: SchemaArrayString}, nil
	case "integer", "number":
		return Schema{Kind: SchemaArrayNumber}, nil
	case "boolean":
		return Schema{Kind: SchemaArrayBoolean}, nil
	default:
		return Schema{}, fmt.Errorf("parameter %q: array items type %q is not a supported primitive", paramName, primaryType(inner.Type))
	}
}

// primaryType returns the first declared type when the schema uses
// OpenAPI 3.1's array-of-types form. Empty input yields an empty string.
func primaryType(types []string) string {
	for _, t := range types {
		if t != "" && t != "null" {
			return t
		}
	}
	return ""
}

// enumStrings coerces a libopenapi Enum slice ([]*yaml.Node) into a
// []string. Numeric enum members round-trip as their YAML-string value.
func enumStrings(values []*yaml.Node) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == nil {
			continue
		}
		out = append(out, v.Value)
	}
	return out
}
