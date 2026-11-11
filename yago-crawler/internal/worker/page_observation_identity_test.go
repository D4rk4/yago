package worker

import (
	"testing"
	"time"
)

func TestPageObservationIdentityIsStableForRetryAndChangesForRecrawl(t *testing.T) {
	firstFetch := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	first := pageObservationIdentity("page", firstFetch)
	if retry := pageObservationIdentity("page", firstFetch); retry != first {
		t.Fatalf("retry observation id = %q, want %q", retry, first)
	}
	if recrawl := pageObservationIdentity("page", firstFetch.Add(time.Second)); recrawl == first {
		t.Fatal("recrawl reused the earlier observation id")
	}
}
