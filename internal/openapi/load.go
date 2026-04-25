package openapi

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Doc is a loaded OpenAPI 3.x document ready to resolve operations by ID.
// Create one per generate run and reuse across all openapi_operation tools
// so the expensive parse + model-build happens once.
type Doc struct {
	model *v3.Document
}

// Load reads path, parses it as OpenAPI 3.x, and returns the usable model.
// OpenAPI 2.0 documents and documents containing remote $ref targets are
// rejected with explicit errors — mcpgen v1 resolves everything locally.
func Load(path string) (*Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if err := rejectSwagger(data); err != nil {
		return nil, err
	}
	if err := rejectRemoteRefs(data); err != nil {
		return nil, err
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	model, buildErr := doc.BuildV3Model()
	if buildErr != nil {
		return nil, fmt.Errorf("build v3 model from %s: %w", path, buildErr)
	}
	if model == nil {
		return nil, fmt.Errorf("parse %s: no v3 model produced (document may not be OpenAPI 3.x)", path)
	}
	return &Doc{model: &model.Model}, nil
}

// rejectSwagger returns a structured error when the document is an OpenAPI
// 2.0 / Swagger file. Done with a lightweight string probe rather than a
// full parse because libopenapi will fail later with a less obvious message.
func rejectSwagger(data []byte) error {
	s := string(data)
	if strings.Contains(s, `"swagger":`) || strings.Contains(s, "\nswagger:") || strings.HasPrefix(strings.TrimSpace(s), "swagger:") {
		return errors.New("OpenAPI 2.0 / Swagger documents are not supported; mcpgen v1 requires OpenAPI 3.x")
	}
	return nil
}

// rejectRemoteRefs is a coarse check for `$ref: "http...` style references.
// libopenapi can resolve them, but DESIGN-0004 explicitly forbids remote
// refs to keep generate runs offline-friendly and reproducible.
func rejectRemoteRefs(data []byte) error {
	s := string(data)
	markers := []string{`"$ref": "http`, `$ref: http`, `$ref: "http`, `$ref: 'http`}
	for _, m := range markers {
		if strings.Contains(s, m) {
			return errors.New("remote $ref in OpenAPI document is not supported; copy referenced schemas inline")
		}
	}
	return nil
}
