package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Record is the demo's domain object. JSON tags use snake_case to match
// the API surface; the Go field names are PascalCase per convention.
type Record struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Message string    `json:"message"`
	Created time.Time `json:"created"`
}

// RecordPatch describes a partial update on POST /api/<auth>/{id}. Both
// fields are optional; nil means "leave unchanged".
type RecordPatch struct {
	Name    *string `json:"name,omitempty"`
	Message *string `json:"message,omitempty"`
}

// CreateInput is the body shape PUT /api/<auth> expects.
type CreateInput struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Store is an in-memory record store guarded by a sync.RWMutex. Pointer
// receiver so callers share a single underlying map; copying a Store
// would split the lock from the data and break invariants.
type Store struct {
	mu      sync.RWMutex
	records map[string]Record
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{records: make(map[string]Record)}
}

// Seed replaces the store contents with the given records. Intended to
// run at startup before the HTTP server begins serving — not safe to
// call concurrently with reads.
func (s *Store) Seed(records []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = make(map[string]Record, len(records))
	for _, r := range records {
		s.records[r.ID] = r
	}
}

// List returns a snapshot of every record, sorted by ID for stable
// output across calls.
func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the record with the given id, if present.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[id]
	return r, ok
}

// Update applies the patch to the record identified by id. Returns the
// updated record on success or false if no such id exists.
func (s *Store) Update(id string, patch RecordPatch) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, false
	}
	if patch.Name != nil {
		r.Name = *patch.Name
	}
	if patch.Message != nil {
		r.Message = *patch.Message
	}
	s.records[id] = r
	return r, true
}

// Create assigns the next sequential rec-NNN id and stores a new record.
func (s *Store) Create(name, message string) Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.nextID()
	r := Record{ID: next, Name: name, Message: message, Created: time.Now()}
	s.records[next] = r
	return r
}

// nextID returns one greater than the highest rec-NNN currently in the
// store. Caller holds s.mu.
func (s *Store) nextID() string {
	highest := 0
	for id := range s.records {
		if !strings.HasPrefix(id, "rec-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(id, "rec-"))
		if err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("rec-%03d", highest+1)
}
