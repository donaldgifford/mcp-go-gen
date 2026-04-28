package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer wires the same mux main.run uses, with a fixed bearer
// token. Returns the *httptest.Server (Close-deferred via t.Cleanup)
// and the bearer token to use for /api/bearer routes.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	store := NewStore()
	store.Seed(SeedRecords())
	const bearer = "test-token"
	srv := httptest.NewServer(buildMux(store, bearer, nil))
	t.Cleanup(srv.Close)
	return srv, bearer
}

// doReq wraps the test client to satisfy noctx. Caller is responsible
// for closing resp.Body — defer it at the call site to keep linters
// (bodyclose) happy.
func doReq(t *testing.T, method, url, contentType string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestListHandler_Noauth(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	resp := doReq(t, http.MethodGet, srv.URL+"/api/noauth", "", http.NoBody)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got listResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Records) != 5 {
		t.Errorf("Records len = %d, want 5", len(got.Records))
	}
}

func TestGetHandler_OKAndNotFound(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	tests := []struct {
		name       string
		id         string
		wantStatus int
	}{
		{"found", "rec-001", http.StatusOK},
		{"missing", "rec-999", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := doReq(t, http.MethodGet, srv.URL+"/api/noauth/"+tc.id, "", http.NoBody)
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestUpdateHandler(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	body := bytes.NewBufferString(`{"name":"renamed"}`)
	resp := doReq(t, http.MethodPost, srv.URL+"/api/noauth/rec-001", "application/json", body)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got Record
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "renamed" {
		t.Errorf("Name = %q, want renamed", got.Name)
	}
	if got.Message != "first demo record" {
		t.Errorf("Message = %q, want unchanged", got.Message)
	}
}

func TestUpdateHandler_BadJSON(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	resp := doReq(t, http.MethodPost, srv.URL+"/api/noauth/rec-001", "application/json",
		strings.NewReader(`{not json`))
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateHandler(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	body := bytes.NewBufferString(`{"name":"zebra","message":"newcomer"}`)
	resp := doReq(t, http.MethodPut, srv.URL+"/api/noauth", "application/json", body)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got Record
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "rec-006" {
		t.Errorf("ID = %q, want rec-006", got.ID)
	}
}

func TestBearerRoutes(t *testing.T) {
	t.Parallel()

	srv, bearer := newTestServer(t)

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"correct token", bearer, http.StatusOK},
		{"wrong token", "wrong-token", http.StatusUnauthorized},
		{"missing token", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/bearer", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	resp := doReq(t, http.MethodGet, srv.URL+"/healthz", "", http.NoBody)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}
