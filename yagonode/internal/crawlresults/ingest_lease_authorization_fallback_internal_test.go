package crawlresults

import (
	"context"
	"errors"
	"testing"
)

func TestSingleIngestRejectsStaleValidationThroughNakFallback(t *testing.T) {
	nacked := 0
	delivery := IngestDelivery{
		ValidateMutation: func(context.Context) error { return errors.New("stale") },
		Nak: func(context.Context) error {
			nacked++

			return nil
		},
	}
	new(IngestConsumer).absorb(t.Context(), delivery)
	if nacked != 1 {
		t.Fatalf("stale delivery nacks = %d, want 1", nacked)
	}
}
