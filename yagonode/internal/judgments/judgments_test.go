package judgments

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

func TestPutNormalizesUpsertsAndListsSorted(t *testing.T) {
	store := openStore(t)

	if err := store.Put(t.Context(), Judgment{
		Query:  "  Linux   Kernel  ",
		Grades: map[string]int{"https://a.example/": 2, "https://b.example/": 0},
	}); err != nil {
		t.Fatalf("Put linux: %v", err)
	}
	if err := store.Put(t.Context(), Judgment{
		Query:  "debian",
		Grades: map[string]int{"https://d.example/": 3},
	}); err != nil {
		t.Fatalf("Put debian: %v", err)
	}
	// Re-curating the normalized query updates the one record rather than adding.
	if err := store.Put(t.Context(), Judgment{
		Query:  "linux kernel",
		Grades: map[string]int{"https://a.example/": 1},
	}); err != nil {
		t.Fatalf("Put linux again: %v", err)
	}

	list, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %d records, want 2 (upsert must not duplicate): %+v", len(list), list)
	}
	if list[0].Query != "debian" || list[1].Query != "linux kernel" {
		t.Fatalf("List not sorted/normalized by query: %+v", list)
	}
	if got := list[1].Grades["https://a.example/"]; got != 1 {
		t.Fatalf("upsert grade = %d, want the updated 1", got)
	}
	if _, present := list[1].Grades["https://b.example/"]; present {
		t.Fatalf("upsert must replace the whole grade map: %+v", list[1].Grades)
	}
}

func TestPutTrimsURLWhitespace(t *testing.T) {
	store := openStore(t)
	paddedURL := "  https://trim.example/  "
	if err := store.Put(t.Context(), Judgment{
		Query:  "trim",
		Grades: map[string]int{paddedURL: 2},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := list[0].Grades["https://trim.example/"]; !ok {
		t.Fatalf("URL whitespace was not trimmed: %+v", list[0].Grades)
	}
}

func TestPutRejectsInvalidJudgments(t *testing.T) {
	store := openStore(t)
	cases := map[string]Judgment{
		"empty query":    {Query: "   ", Grades: map[string]int{"https://a/": 1}},
		"no graded url":  {Query: "q", Grades: map[string]int{}},
		"empty url":      {Query: "q", Grades: map[string]int{"  ": 1}},
		"negative grade": {Query: "q", Grades: map[string]int{"https://a/": -1}},
	}
	for name, judgment := range cases {
		if err := store.Put(t.Context(), judgment); err == nil {
			t.Errorf("%s: Put should have failed", name)
		}
	}
}

func TestDeleteReportsPresenceAndRejectsEmpty(t *testing.T) {
	store := openStore(t)
	if err := store.Put(t.Context(), Judgment{
		Query:  "ubuntu",
		Grades: map[string]int{"https://u.example/": 2},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	removed, err := store.Delete(t.Context(), "  UBUNTU ")
	if err != nil || !removed {
		t.Fatalf("Delete existing = (%v, %v), want (true, nil)", removed, err)
	}
	removed, err = store.Delete(t.Context(), "ubuntu")
	if err != nil || removed {
		t.Fatalf("Delete absent = (%v, %v), want (false, nil)", removed, err)
	}
	if _, err := store.Delete(t.Context(), "   "); err == nil {
		t.Fatalf("Delete empty query should have failed")
	}
}

func TestListEmptyStore(t *testing.T) {
	store := openStore(t)
	list, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("fresh store List = %+v, want empty", list)
	}
}

func TestOpenRejectsDuplicateBucket(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := Open(v); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatalf("second Open on the same vault must fail (duplicate bucket)")
	}
}

func TestStoreSurfacesVaultErrors(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	valid := Judgment{Query: "q", Grades: map[string]int{"https://u.example/": 1}}
	if err := store.Put(t.Context(), valid); err == nil {
		t.Fatalf("Put on a closed vault must fail")
	}
	if _, err := store.Delete(t.Context(), "q"); err == nil {
		t.Fatalf("Delete on a closed vault must fail")
	}
	if _, err := store.List(t.Context()); err == nil {
		t.Fatalf("List on a closed vault must fail")
	}
}

func TestCodecDecodeRejectsGarbage(t *testing.T) {
	if _, err := (judgmentCodec{}).Decode([]byte("{not json")); err == nil {
		t.Fatalf("Decode of malformed bytes should fail")
	}
	// Encode always succeeds for the plain struct; round-trip a value to cover it.
	raw, err := (judgmentCodec{}).Encode(Judgment{Query: "q", Grades: map[string]int{"u": 1}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := (judgmentCodec{}).Decode(raw); err != nil {
		t.Fatalf("Decode of encoded value: %v", err)
	}
}
