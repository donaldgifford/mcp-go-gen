package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// listResponse is the wire shape of GET /api/<auth>.
type listResponse struct {
	Records []Record `json:"records"`
}

// errorResponse is the wire shape of every 4xx body.
type errorResponse struct {
	Error string `json:"error"`
	ID    string `json:"id,omitempty"`
}

// listHandler returns every record in the store as a JSON list.
func listHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, listResponse{Records: store.List()})
	}
}

// getHandler returns a single record by id; 404 if absent.
func getHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		rec, ok := store.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "record not found", ID: id})
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// updateHandler applies a partial update from the JSON body.
func updateHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var patch RecordPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		updated, ok := store.Update(id, patch)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "record not found", ID: id})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// createHandler creates a new record from the JSON body.
func createHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input CreateInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, store.Create(input.Name, input.Message))
	}
}

// writeJSON serializes v with the given status. Encoding failures land
// in the access log but cannot be surfaced to the client (headers may
// have been flushed already).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json", "err", err)
	}
}
