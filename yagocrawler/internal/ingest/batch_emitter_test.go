package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
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
	document := yagocrawlcontract.DocumentIngest{NormalizedURL: "http://example.com/"}
	envelope := ingest.Envelope{
		SourceURL:     "http://example.com/",
		Provenance:    []byte("peer"),
		ProfileHandle: "handle",
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
