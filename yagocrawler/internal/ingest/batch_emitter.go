package ingest

import (
	"context"
	"fmt"

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
		Document:      document,
		Postings:      postings,
		Metadata:      []yagomodel.URIMetadataRow{metadata},
	}
	if err := e.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", envelope.SourceURL, err)
	}
	return nil
}
