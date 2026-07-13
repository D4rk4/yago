package ingest

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagomodel"
)

type Envelope struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
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
		sourceURL string,
		provenance []byte,
		profileHandle string,
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
		SourceURL:     envelope.SourceURL,
		Provenance:    envelope.Provenance,
		ProfileHandle: envelope.ProfileHandle,
		ObservationID: rand.Text(),
		ObservedAt:    ingestObservationTime(document.FetchedAt),
		Document:      document,
		Postings:      postings,
		Metadata:      []yagomodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}

func (e *batchEmitter) EmitRemoval(
	ctx context.Context,
	sourceURL string,
	provenance []byte,
	profileHandle string,
) error {
	batch := IngestBatch{
		SourceURL:     sourceURL,
		Provenance:    provenance,
		ProfileHandle: profileHandle,
		ObservationID: rand.Text(),
		ObservedAt:    time.Now().UTC(),
		Removed:       true,
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish removal batch %s: %w", sourceURL, err)
	}
	return nil
}

func ingestObservationTime(fetchedAt time.Time) time.Time {
	if !fetchedAt.IsZero() {
		return fetchedAt.UTC()
	}

	return time.Now().UTC()
}
