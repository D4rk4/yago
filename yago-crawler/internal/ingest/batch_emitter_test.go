package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type failingPublisher struct {
	err error
}

func (p failingPublisher) Publish(context.Context, ingest.IngestBatch) error {
	return p.err
}

func TestBatchEmitterAssemblesEnvelope(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[ingest.IngestBatch](1)
	emitter := ingest.NewBatchEmitter(queue)

	postings := []yagomodel.RWIPosting{{WordHash: yagomodel.WordHash("kangaroo")}}
	metadata := yagomodel.URIMetadataRow{Properties: map[string]string{"u": "x"}}
	fetchedAt := time.Date(2026, 7, 13, 8, 30, 0, 0, time.FixedZone("test", 3600))
	document := yagocrawlcontract.DocumentIngest{
		NormalizedURL: "http://example.com/",
		FetchedAt:     fetchedAt,
	}
	envelope := ingest.Envelope{
		SourceURL:        "http://example.com/",
		Provenance:       []byte("peer"),
		ProfileHandle:    "handle",
		ObservationID:    "stable-observation",
		ObservedAt:       fetchedAt.Add(-time.Hour),
		SourceModifiedAt: fetchedAt.Add(-2 * time.Hour),
	}

	if err := emitter.Emit(
		context.Background(),
		document,
		postings,
		metadata,
		envelope,
	); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	batch := <-queue.Receive()
	if batch.SourceURL != envelope.SourceURL {
		t.Errorf("source url = %q", batch.SourceURL)
	}
	if batch.ProfileHandle != envelope.ProfileHandle {
		t.Errorf("handle = %q", batch.ProfileHandle)
	}
	if len(batch.Postings) != 1 || batch.Postings[0].WordHash != yagomodel.WordHash("kangaroo") {
		t.Errorf("postings = %v", batch.Postings)
	}
	if len(batch.Metadata) != 1 {
		t.Errorf("metadata rows = %d, want 1", len(batch.Metadata))
	}
	if batch.Document.NormalizedURL != document.NormalizedURL {
		t.Errorf("document = %#v", batch.Document)
	}
	if batch.ObservationID != envelope.ObservationID {
		t.Errorf("observation id = %q, want %q", batch.ObservationID, envelope.ObservationID)
	}
	if !batch.ObservedAt.Equal(envelope.ObservedAt) || batch.ObservedAt.Location() != time.UTC {
		t.Errorf("observed at = %v, want %v in UTC", batch.ObservedAt, envelope.ObservedAt)
	}
	if !batch.SourceModifiedAt.Equal(envelope.SourceModifiedAt) {
		t.Errorf("source modified at = %v, want %v", batch.SourceModifiedAt,
			envelope.SourceModifiedAt)
	}
}

func TestBatchEmitterReturnsPublishError(t *testing.T) {
	sentinel := errors.New("queue closed")
	emitter := ingest.NewBatchEmitter(failingPublisher{err: sentinel})

	err := emitter.Emit(
		context.Background(),
		yagocrawlcontract.DocumentIngest{},
		nil,
		yagomodel.URIMetadataRow{},
		ingest.Envelope{SourceURL: "http://example.com/"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Emit error = %v, want %v", err, sentinel)
	}
}

func TestBatchEmitterUsesFetchTimeWhenObservationTimeIsAbsent(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[ingest.IngestBatch](1)
	emitter := ingest.NewBatchEmitter(queue)
	fetchedAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.FixedZone("test", 7200))
	if err := emitter.Emit(
		t.Context(),
		yagocrawlcontract.DocumentIngest{FetchedAt: fetchedAt},
		nil,
		yagomodel.URIMetadataRow{},
		ingest.Envelope{SourceURL: "https://example.org/page"},
	); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	batch := <-queue.Receive()
	if !batch.ObservedAt.Equal(fetchedAt) || batch.ObservedAt.Location() != time.UTC {
		t.Fatalf("observed at = %v, want %v in UTC", batch.ObservedAt, fetchedAt)
	}
}

func TestBatchEmitterEmitsRemovalTombstone(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[ingest.IngestBatch](1)
	emitter := ingest.NewBatchEmitter(queue)

	if err := emitter.EmitRemoval(
		context.Background(),
		ingest.Envelope{
			SourceURL:     "http://example.com/gone",
			Provenance:    []byte("peer"),
			ProfileHandle: "handle",
			ObservationID: "stable-removal",
			ObservedAt:    time.Date(2026, 7, 16, 8, 0, 0, 0, time.FixedZone("test", 3600)),
		},
	); err != nil {
		t.Fatalf("EmitRemoval: %v", err)
	}

	batch := <-queue.Receive()
	if !batch.Removed {
		t.Fatalf("removal batch must set Removed: %#v", batch)
	}
	if batch.SourceURL != "http://example.com/gone" ||
		string(batch.Provenance) != "peer" || batch.ProfileHandle != "handle" {
		t.Fatalf("removal envelope = %#v", batch)
	}
	if batch.Document.NormalizedURL != "" ||
		len(batch.Postings) != 0 || len(batch.Metadata) != 0 {
		t.Fatalf("removal batch must be empty of content: %#v", batch)
	}
	if batch.ObservationID != "stable-removal" ||
		!batch.ObservedAt.Equal(time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)) ||
		batch.ObservedAt.Location() != time.UTC {
		t.Fatalf("removal observation identity is incomplete: %#v", batch)
	}
}

func TestBatchEmitterReturnsRemovalPublishError(t *testing.T) {
	sentinel := errors.New("queue closed")
	emitter := ingest.NewBatchEmitter(failingPublisher{err: sentinel})

	err := emitter.EmitRemoval(context.Background(), ingest.Envelope{
		SourceURL:     "http://example.com/gone",
		ProfileHandle: "handle",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("EmitRemoval error = %v, want %v", err, sentinel)
	}
}
