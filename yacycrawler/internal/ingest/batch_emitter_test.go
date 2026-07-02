package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yacycrawler/internal/ingest"
	"github.com/D4rk4/yago/yacymodel"
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

	postings := []yacymodel.RWIPosting{{WordHash: yacymodel.WordHash("kangaroo")}}
	metadata := yacymodel.URIMetadataRow{Properties: map[string]string{"u": "x"}}
	envelope := ingest.Envelope{
		SourceURL:     "http://example.com/",
		Provenance:    []byte("peer"),
		ProfileHandle: "handle",
	}

	if err := emitter.Emit(context.Background(), postings, metadata, envelope); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	batch := <-queue.Receive()
	if batch.SourceURL != envelope.SourceURL {
		t.Errorf("source url = %q", batch.SourceURL)
	}
	if batch.ProfileHandle != envelope.ProfileHandle {
		t.Errorf("handle = %q", batch.ProfileHandle)
	}
	if len(batch.Postings) != 1 || batch.Postings[0].WordHash != yacymodel.WordHash("kangaroo") {
		t.Errorf("postings = %v", batch.Postings)
	}
	if len(batch.Metadata) != 1 {
		t.Errorf("metadata rows = %d, want 1", len(batch.Metadata))
	}
}

func TestBatchEmitterReturnsPublishError(t *testing.T) {
	sentinel := errors.New("queue closed")
	emitter := ingest.NewBatchEmitter(failingPublisher{err: sentinel})

	err := emitter.Emit(
		context.Background(),
		nil,
		yacymodel.URIMetadataRow{},
		ingest.Envelope{SourceURL: "http://example.com/"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Emit error = %v, want %v", err, sentinel)
	}
}
