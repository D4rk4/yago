package ingest

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/boundedqueue"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Envelope struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
}

type BatchEmitter interface {
	Emit(
		ctx context.Context,
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
	postings []yacymodel.RWIPosting,
	metadata yacymodel.URIMetadataRow,
	envelope Envelope,
) error {
	batch := IngestBatch{
		SourceURL:     envelope.SourceURL,
		Provenance:    envelope.Provenance,
		ProfileHandle: envelope.ProfileHandle,
		Postings:      postings,
		Metadata:      []yacymodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}
