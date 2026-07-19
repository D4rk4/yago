package yagonode

import (
	"context"
	"testing"
)

func TestRemoteSearchResourceTallyAddsOneIndexAndURLPerResource(t *testing.T) {
	tally := openTestTransferTally(t)
	if err := tally.AddReceivedWords(t.Context(), 2); err != nil {
		t.Fatalf("AddReceivedWords: %v", err)
	}
	if err := tally.AddReceivedURLs(t.Context(), 1); err != nil {
		t.Fatalf("AddReceivedURLs: %v", err)
	}

	observe := remoteSearchResourceTally(tally)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	observe(canceled, 3)

	totals, err := tally.Totals(t.Context())
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.ReceivedWords != 5 || totals.ReceivedURLs != 4 {
		t.Fatalf("received totals = %d/%d, want 5/4", totals.ReceivedWords, totals.ReceivedURLs)
	}
}

func TestPublicRemoteSearchConfigCarriesReceivedResourceObservation(t *testing.T) {
	var resources int
	config := publicRemoteSearchConfig(publicSearchAssembly{
		observeRemoteResources: func(_ context.Context, received int) {
			resources += received
		},
	})
	if config.ObserveReceivedResources == nil {
		t.Fatal("received resource observation was dropped")
	}
	config.ObserveReceivedResources(t.Context(), 2)
	if resources != 2 {
		t.Fatalf("received resources = %d, want 2", resources)
	}
}

func TestRemoteSearchResourceTallyIsAbsentWithoutStorage(t *testing.T) {
	if remoteSearchResourceTally(nil) != nil {
		t.Fatal("nil transfer tally produced an observer")
	}
}
