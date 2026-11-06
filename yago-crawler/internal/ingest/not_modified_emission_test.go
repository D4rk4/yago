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

func storedRevisionObservation() yagocrawlcontract.NotModifiedObservation {
	return yagocrawlcontract.NotModifiedObservation{
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 3,
			FetchedAt:            time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC),
		},
	}
}

func revalidatedEnvelope(validators *yagocrawlcontract.HTTPValidators) ingest.Envelope {
	return ingest.Envelope{
		SourceURL:          "https://example.org/revalidated",
		Provenance:         []byte("run"),
		ProfileHandle:      "handle",
		ObservationID:      "observation-revalidated",
		ResponseValidators: validators,
	}
}

func TestEmittersAttachValidResponseValidatorsUnchanged(t *testing.T) {
	validators := &yagocrawlcontract.HTTPValidators{
		ETag:         `W/"v3-<&>"`,
		LastModified: "Sunday, 01-Nov-26 06:00:00 GMT",
	}
	for _, batch := range emittedBatches(t, revalidatedEnvelope(validators)) {
		if batch.ResponseValidators == nil || *batch.ResponseValidators != *validators {
			t.Fatalf("response validators = %+v, want %+v", batch.ResponseValidators, validators)
		}
	}
}

func TestEmittersDropInvalidResponseValidators(t *testing.T) {
	invalid := &yagocrawlcontract.HTTPValidators{ETag: "\"v3\"\r\nSet-Cookie: x=1"}
	for _, batch := range emittedBatches(t, revalidatedEnvelope(invalid)) {
		if batch.ResponseValidators != nil {
			t.Fatalf("invalid response validators attached: %+v", batch.ResponseValidators)
		}
		if err := batch.ValidatePayload(); err != nil {
			t.Fatalf("emitted batch is invalid: %v", err)
		}
	}
}

func TestEmitNotModifiedPublishesObservation(t *testing.T) {
	batches := emittedBatches(t, revalidatedEnvelope(nil))
	notModified := batches[1]
	if notModified.NotModified == nil || *notModified.NotModified != storedRevisionObservation() {
		t.Fatalf("not-modified observation = %+v", notModified.NotModified)
	}
	if notModified.SourceURL != "https://example.org/revalidated" ||
		notModified.ObservationID != "observation-revalidated" {
		t.Fatalf("not-modified identity = %+v", notModified)
	}
	if batches[0].NotModified != nil {
		t.Fatalf("document batch carries a not-modified observation: %+v", batches[0].NotModified)
	}
}

func TestEmitNotModifiedWrapsPublishFailure(t *testing.T) {
	publishFailure := errors.New("node unavailable")
	emitter := ingest.NewBatchEmitter(failingPublisher{err: publishFailure})

	err := emitter.EmitNotModified(
		context.Background(),
		storedRevisionObservation(),
		revalidatedEnvelope(nil),
	)

	if !errors.Is(err, publishFailure) {
		t.Fatalf("emit not-modified error = %v, want wrapped publish failure", err)
	}
}

func emittedBatches(t *testing.T, envelope ingest.Envelope) []ingest.IngestBatch {
	t.Helper()
	queue := boundedqueue.NewBoundedQueue[ingest.IngestBatch](2)
	emitter := ingest.NewBatchEmitter(queue)
	if err := emitter.Emit(
		context.Background(),
		yagocrawlcontract.DocumentIngest{NormalizedURL: envelope.SourceURL},
		nil,
		yagomodel.URIMetadataRow{},
		envelope,
	); err != nil {
		t.Fatalf("emit document: %v", err)
	}
	if err := emitter.EmitNotModified(
		context.Background(),
		storedRevisionObservation(),
		envelope,
	); err != nil {
		t.Fatalf("emit not-modified: %v", err)
	}

	return []ingest.IngestBatch{<-queue.Receive(), <-queue.Receive()}
}
