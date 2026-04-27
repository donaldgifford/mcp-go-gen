package main

import "time"

// SeedRecords returns the canonical 5 records the demo API loads at
// startup. The same set is shared across all auth trees so calling
// list_noauth_records and list_bearer_records returns identical data —
// the demo's storyline is "same store, different auth perimeters".
func SeedRecords() []Record {
	now := time.Now()
	return []Record{
		{ID: "rec-001", Name: "alpha", Message: "first demo record", Created: now},
		{ID: "rec-002", Name: "bravo", Message: "second demo record", Created: now},
		{ID: "rec-003", Name: "charlie", Message: "third demo record", Created: now},
		{ID: "rec-004", Name: "delta", Message: "fourth demo record", Created: now},
		{ID: "rec-005", Name: "echo", Message: "fifth demo record", Created: now},
	}
}
