package searchindex

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// TestStoredCandidateProjectionKeysArePersistedContract pins the serialized key
// names, which round-trip tests structurally cannot: encode and decode share one
// struct, so renaming a tag changes both sides at once and every round-trip stays
// green while every payload already on disk becomes unreadable under that name.
// A silently renamed key does not fail, it degrades -- the reader sees an absent
// field, and for the first-seen pair that means every stored candidate declines
// support and falls back to a full document load.
func TestStoredCandidateProjectionKeysArePersistedContract(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(newStoredCandidateProjection(documentstore.Document{
		FirstSeenAt: when,
	}))
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keyed); err != nil {
		t.Fatalf("unmarshal projection: %v", err)
	}

	// The first-seen pair this release added. "f" carries the time and "fc"
	// records that the writer knew the field at all, which is how a payload
	// written before the field existed is recognised and routed to the stored
	// document instead of being read as an unseen document.
	for _, key := range []string{"f", "fc"} {
		if _, ok := keyed[key]; !ok {
			t.Fatalf("persisted key %q is absent; keys = %v", key, slices.Sorted(maps(keyed)))
		}
	}
	var firstSeen time.Time
	if err := json.Unmarshal(keyed["f"], &firstSeen); err != nil {
		t.Fatalf("decode persisted first-seen: %v", err)
	}
	if !firstSeen.Equal(when) {
		t.Fatalf("persisted first-seen = %v, want %v", firstSeen, when)
	}

	// A document with no first-seen time still persists the key: `omitempty` has
	// no effect on a time.Time, because encoding/json applies it to strings, maps
	// and slices but never to a struct. Every time field in this projection is
	// tagged that way, so the tag documents an intent the encoder does not honour
	// and each payload carries a zero timestamp. That costs bytes, not
	// correctness -- an absent key and a zero time decode alike -- and "fc" is
	// what actually distinguishes a pre-field payload. Pinned here so the
	// discrepancy is recorded rather than rediscovered.
	bare, err := json.Marshal(newStoredCandidateProjection(documentstore.Document{}))
	if err != nil {
		t.Fatalf("marshal bare projection: %v", err)
	}
	var bareKeyed map[string]json.RawMessage
	if err := json.Unmarshal(bare, &bareKeyed); err != nil {
		t.Fatalf("unmarshal bare projection: %v", err)
	}
	if _, ok := bareKeyed["fc"]; !ok {
		t.Fatal("the writer-knew-the-field marker must persist even when empty")
	}
	var bareFirstSeen time.Time
	if err := json.Unmarshal(bareKeyed["f"], &bareFirstSeen); err != nil {
		t.Fatalf("decode bare first-seen: %v", err)
	}
	if !bareFirstSeen.IsZero() {
		t.Fatalf("bare first-seen = %v, want the zero time", bareFirstSeen)
	}
}

func maps(m map[string]json.RawMessage) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range m {
			if !yield(key) {
				return
			}
		}
	}
}
