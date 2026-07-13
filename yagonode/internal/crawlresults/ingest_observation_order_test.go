package crawlresults_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func observationBatch(
	observationID string,
	observedAt time.Time,
	removed bool,
) yagocrawlcontract.IngestBatch {
	const sourceURL = "https://example.org/page"
	batch := yagocrawlcontract.IngestBatch{
		SourceURL:     sourceURL,
		ObservationID: observationID,
		ObservedAt:    observedAt,
		Removed:       removed,
	}
	if !removed {
		batch.Document = yagocrawlcontract.DocumentIngest{
			NormalizedURL: sourceURL,
			ExtractedText: observationID,
			FetchedAt:     observedAt,
		}
	}

	return batch
}

func startObservationConsumer(
	history *crawlresults.URLObservationHistory,
	documents *recordingDocumentReceiver,
	purger *recordingPurger,
) (chan<- crawlresults.IngestDelivery, func()) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		documents,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.OrderObservations(history)
	consumer.PurgeURLs(purger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()

	return stream.out, func() {
		cancel()
		<-done
	}
}

func settleObservation(
	t *testing.T,
	input chan<- crawlresults.IngestDelivery,
	batch yagocrawlcontract.IngestBatch,
	ackErr error,
) string {
	t.Helper()
	settled := make(chan string, 1)
	input <- crawlresults.IngestDelivery{
		Batch: batch,
		Ack: func(context.Context) error {
			settled <- "ack"

			return ackErr
		},
		Nak: func(context.Context) error {
			settled <- "nak"

			return nil
		},
	}
	select {
	case result := <-settled:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("observation did not settle")

		return ""
	}
}

func TestSeparateDeliveriesSkipOlderLiveAndTombstoneObservations(t *testing.T) {
	base := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		first      yagocrawlcontract.IngestBatch
		second     yagocrawlcontract.IngestBatch
		wantDocs   int
		wantPurges int
	}{
		{
			name:       "older tombstone after live page",
			first:      observationBatch("live-new", base.Add(time.Hour), false),
			second:     observationBatch("gone-old", base, true),
			wantDocs:   1,
			wantPurges: 0,
		},
		{
			name:       "older live page after tombstone",
			first:      observationBatch("gone-new", base.Add(time.Hour), true),
			second:     observationBatch("live-old", base, false),
			wantDocs:   0,
			wantPurges: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
			purger := &recordingPurger{}
			input, stop := startObservationConsumer(history, documents, purger)
			t.Cleanup(stop)

			if result := settleObservation(t, input, test.first, nil); result != "ack" {
				t.Fatalf("first settlement = %q, want ack", result)
			}
			if result := settleObservation(t, input, test.second, nil); result != "ack" {
				t.Fatalf("second settlement = %q, want ack", result)
			}
			if documents.calls != test.wantDocs || purger.calls() != test.wantPurges {
				t.Fatalf("documents=%d purges=%d, want %d/%d",
					documents.calls, purger.calls(), test.wantDocs, test.wantPurges)
			}
		})
	}
}

func TestCommittedObservationSurvivesLostAckAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.db")
	batch := observationBatch(
		"stable-observation",
		time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC),
		false,
	)
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := crawlresults.OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}
	documents := &recordingDocumentReceiver{}
	input, stop := startObservationConsumer(history, documents, &recordingPurger{})
	if result := settleObservation(t, input, batch, errors.New("ack lost")); result != "ack" {
		t.Fatalf("first settlement = %q, want ack callback", result)
	}
	stop()
	if documents.calls != 1 {
		t.Fatalf("first document writes = %d, want 1", documents.calls)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	history, err = crawlresults.OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}
	replayedDocuments := &recordingDocumentReceiver{}
	input, stop = startObservationConsumer(history, replayedDocuments, &recordingPurger{})
	t.Cleanup(stop)
	if result := settleObservation(t, input, batch, nil); result != "ack" {
		t.Fatalf("replay settlement = %q, want ack", result)
	}
	if replayedDocuments.calls != 0 {
		t.Fatalf("replayed document writes = %d, want 0", replayedDocuments.calls)
	}
}

func TestIncompleteObservationRetriesSideEffects(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	history, err := crawlresults.OpenURLObservationHistory(storage)
	if err != nil {
		t.Fatal(err)
	}
	documents := &recordingDocumentReceiver{err: errors.New("write failed")}
	input, stop := startObservationConsumer(history, documents, &recordingPurger{})
	t.Cleanup(stop)
	batch := observationBatch(
		"retry-observation",
		time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		false,
	)
	if result := settleObservation(t, input, batch, nil); result != "nak" {
		t.Fatalf("failed settlement = %q, want nak", result)
	}
	documents.err = nil
	if result := settleObservation(t, input, batch, nil); result != "ack" {
		t.Fatalf("retry settlement = %q, want ack", result)
	}
	if documents.calls != 2 || documents.doc.ExtractedText != "retry-observation" {
		t.Fatalf("document writes=%d document=%#v", documents.calls, documents.doc)
	}
}
