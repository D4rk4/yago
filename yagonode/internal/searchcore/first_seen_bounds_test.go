package searchcore

import (
	"testing"
	"time"
)

// TestRequestFirstSeenBounded pins both halves of the window independently. An
// end-only window is a legal request -- "everything discovered up to this day"
// -- so reading only the start would silently serve it unfiltered.
func TestRequestFirstSeenBounded(t *testing.T) {
	when := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	if (Request{}).FirstSeenBounded() {
		t.Fatal("a request with no first-seen window must not be bounded")
	}
	if !(Request{MinFirstSeen: when}).FirstSeenBounded() {
		t.Fatal("a start-only first-seen window must be bounded")
	}
	if !(Request{MaxFirstSeen: when}).FirstSeenBounded() {
		t.Fatal("an end-only first-seen window must be bounded")
	}
	if !(Request{MinFirstSeen: when, MaxFirstSeen: when.Add(time.Hour)}).FirstSeenBounded() {
		t.Fatal("a closed first-seen window must be bounded")
	}
	// A publication window is a different dimension and never implies this one.
	if (Request{MinDate: when, MaxDate: when.Add(time.Hour)}).FirstSeenBounded() {
		t.Fatal("a publication window must not report a first-seen bound")
	}
}
