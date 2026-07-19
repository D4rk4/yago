package ingest

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type Envelope struct {
	SourceURL        string
	Provenance       []byte
	ProfileHandle    string
	ObservationID    string
	ObservedAt       time.Time
	SourceModifiedAt time.Time
}

type BatchEmitter interface {
	Emit(
		ctx context.Context,
		document yagocrawlcontract.DocumentIngest,
		postings []yagomodel.RWIPosting,
		metadata yagomodel.URIMetadataRow,
		envelope Envelope,
	) error
	// EmitRemoval publishes a tombstone (Removed) batch for a URL a recrawl
	// found permanently gone, so the node purges its index entry (ADR-0034). It
	// flows through the same durable at-least-once publisher as Emit.
	EmitRemoval(
		ctx context.Context,
		envelope Envelope,
	) error
}

type batchEmitter struct {
	queue boundedqueue.Publisher[IngestBatch]
}

func NewBatchEmitter(queue boundedqueue.Publisher[IngestBatch]) BatchEmitter {
	return &batchEmitter{queue: queue}
}

func (e *batchEmitter) Emit(
	ctx context.Context,
	document yagocrawlcontract.DocumentIngest,
	postings []yagomodel.RWIPosting,
	metadata yagomodel.URIMetadataRow,
	envelope Envelope,
) error {
	batch := IngestBatch{
		SourceURL:        envelope.SourceURL,
		Provenance:       envelope.Provenance,
		ProfileHandle:    envelope.ProfileHandle,
		ObservationID:    ingestObservationID(envelope.ObservationID),
		ObservedAt:       ingestObservationTime(envelope.ObservedAt, document.FetchedAt),
		SourceModifiedAt: envelope.SourceModifiedAt,
		Document:         document,
		Postings:         postings,
		Metadata:         []yagomodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}

func (e *batchEmitter) EmitRemoval(
	ctx context.Context,
	envelope Envelope,
) error {
	batch := IngestBatch{
		SourceURL:     envelope.SourceURL,
		Provenance:    envelope.Provenance,
		ProfileHandle: envelope.ProfileHandle,
		ObservationID: ingestObservationID(envelope.ObservationID),
		ObservedAt:    ingestObservationTime(envelope.ObservedAt, time.Time{}),
		Removed:       true,
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish removal batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}

func ingestObservationID(observationID string) string {
	if observationID != "" {
		return observationID
	}

	return rand.Text()
}

func ingestObservationTime(observedAt, fetchedAt time.Time) time.Time {
	if !observedAt.IsZero() {
		return observedAt.UTC()
	}
	if !fetchedAt.IsZero() {
		return fetchedAt.UTC()
	}

	return time.Now().UTC()
}
