package searchlocal

import (
	"testing"
	"time"
)

func TestCompactPublicationDateOmitsUnknownTime(t *testing.T) {
	for _, published := range []time.Time{{}, time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)} {
		if got := compactPublicationDate(published); got != "" {
			t.Fatalf("unknown publication date = %q, want empty", got)
		}
	}
}

func TestCompactPublicationDateUsesCanonicalUTCDay(t *testing.T) {
	published := time.Date(
		2026,
		time.July,
		2,
		1,
		0,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)
	if got := compactPublicationDate(published); got != "20260701" {
		t.Fatalf("publication date = %q, want UTC day", got)
	}
}
