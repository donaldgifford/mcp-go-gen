package main

import (
	"testing"
)

func TestStore_SeedAndList(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Seed(SeedRecords())

	got := s.List()
	if len(got) != 5 {
		t.Fatalf("List() = %d records, want 5", len(got))
	}

	wantIDs := []string{"rec-001", "rec-002", "rec-003", "rec-004", "rec-005"}
	for i, w := range wantIDs {
		if got[i].ID != w {
			t.Errorf("List()[%d].ID = %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestStore_Get(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Seed(SeedRecords())

	tests := []struct {
		name      string
		id        string
		wantFound bool
		wantName  string
	}{
		{"existing", "rec-002", true, "bravo"},
		{"missing", "rec-999", false, ""},
		{"empty", "", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, ok := s.Get(tc.id)
			if ok != tc.wantFound {
				t.Fatalf("Get(%q) found = %v, want %v", tc.id, ok, tc.wantFound)
			}
			if ok && rec.Name != tc.wantName {
				t.Errorf("Get(%q).Name = %q, want %q", tc.id, rec.Name, tc.wantName)
			}
		})
	}
}

func TestStore_UpdatePartial(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Seed(SeedRecords())

	newName := "renamed"
	updated, ok := s.Update("rec-001", RecordPatch{Name: &newName})
	if !ok {
		t.Fatal("Update(rec-001) found = false, want true")
	}
	if updated.Name != "renamed" {
		t.Errorf("Update.Name = %q, want renamed", updated.Name)
	}
	if updated.Message != "first demo record" {
		t.Errorf("Update.Message = %q, want unchanged", updated.Message)
	}

	got, _ := s.Get("rec-001")
	if got.Name != "renamed" {
		t.Errorf("Get.Name = %q, want renamed (Update did not persist)", got.Name)
	}
}

func TestStore_UpdateMissing(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Seed(SeedRecords())

	name := "x"
	_, ok := s.Update("rec-999", RecordPatch{Name: &name})
	if ok {
		t.Error("Update(rec-999) found = true, want false")
	}
}

func TestStore_Create_AssignsNextID(t *testing.T) {
	t.Parallel()

	s := NewStore()
	s.Seed(SeedRecords())

	rec := s.Create("zebra", "newcomer")
	if rec.ID != "rec-006" {
		t.Errorf("Create.ID = %q, want rec-006", rec.ID)
	}
	if rec.Name != "zebra" {
		t.Errorf("Create.Name = %q, want zebra", rec.Name)
	}

	rec2 := s.Create("yak", "another")
	if rec2.ID != "rec-007" {
		t.Errorf("second Create.ID = %q, want rec-007", rec2.ID)
	}

	if got := s.List(); len(got) != 7 {
		t.Errorf("List() = %d records after 2 creates, want 7", len(got))
	}
}

func TestStore_Create_OnEmpty(t *testing.T) {
	t.Parallel()

	s := NewStore()
	rec := s.Create("first", "from empty")
	if rec.ID != "rec-001" {
		t.Errorf("Create on empty store ID = %q, want rec-001", rec.ID)
	}
}
