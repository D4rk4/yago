package crawlresults_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func storedDocumentsAfterSourceOnlyObservation(
	t *testing.T,
	sourceOnly yagocrawlcontract.IngestBatch,
	fallback yagocrawlcontract.IngestBatch,
) int {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	history, err := crawlresults.OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}
	documents := &recordingDocumentReceiver{}
	input, stop := startObservationConsumer(history, documents, &recordingPurger{})
	t.Cleanup(stop)
	if result := settleObservation(t, input, sourceOnly, nil); result != "ack" {
		t.Fatalf("source-only settlement = %q, want ack", result)
	}
	if result := settleObservation(t, input, fallback, nil); result != "ack" {
		t.Fatalf("fallback settlement = %q, want ack", result)
	}

	return documents.calls
}

func TestFallbackDocumentAfterSourceOnlyNotModifiedObservationIsStored(t *testing.T) {
	pageObservedAt := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	notModified := yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/page",
		ObservationID: "not-modified-observation",
		ObservedAt:    pageObservedAt.Add(-time.Nanosecond),
	}
	fallback := observationBatch("page-observation", pageObservedAt, false)

	if stored := storedDocumentsAfterSourceOnlyObservation(t, notModified, fallback); stored != 1 {
		t.Fatalf("documents stored after fallback = %d, want 1", stored)
	}
}

func TestFallbackDocumentWithTheSourceOnlyIdentityIsDropped(t *testing.T) {
	pageObservedAt := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	sameIdentity := yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/page",
		ObservationID: "page-observation",
		ObservedAt:    pageObservedAt,
	}
	fallback := observationBatch("page-observation", pageObservedAt, false)

	if stored := storedDocumentsAfterSourceOnlyObservation(t, sameIdentity, fallback); stored != 0 {
		t.Fatalf("documents stored after same-identity fallback = %d, want 0", stored)
	}
}
