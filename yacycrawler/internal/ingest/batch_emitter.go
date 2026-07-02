package ingest

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yacymodel"
)

type Envelope struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
}

type BatchEmitter interface {
	Emit(
		ctx context.Context,
		document yacycrawlcontract.DocumentIngest,
		postings []yacymodel.RWIPosting,
		metadata yacymodel.URIMetadataRow,
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
	document yacycrawlcontract.DocumentIngest,
	postings []yacymodel.RWIPosting,
	metadata yacymodel.URIMetadataRow,
	envelope Envelope,
) error {
	batch := IngestBatch{
		SourceURL:     envelope.SourceURL,
		Provenance:    envelope.Provenance,
		ProfileHandle: envelope.ProfileHandle,
		Document:      document,
		Postings:      postings,
		Metadata:      []yacymodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}
