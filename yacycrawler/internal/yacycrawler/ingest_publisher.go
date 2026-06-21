package yacycrawler

import (
	"context"
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type IngestPublisher struct {
	queue Publisher[IngestBatch]
	clock func() time.Time
}

func NewIngestPublisher(queue Publisher[IngestBatch]) *IngestPublisher {
	return &IngestPublisher{queue: queue, clock: time.Now}
}

func (p *IngestPublisher) Publish(
	ctx context.Context,
	page ParsedPage,
	profileHandle string,
	provenance []byte,
) error {
	stats := BuildPageStats(page)
	postings, err := buildPostings(page, stats)
	if err != nil {
		return err
	}
	metadata, err := buildMetadata(page, stats, p.clock())
	if err != nil {
		return err
	}
	batch := IngestBatch{
		SourceURL:     page.URL,
		Provenance:    provenance,
		ProfileHandle: profileHandle,
		Postings:      postings,
		Metadata:      []yacymodel.URIMetadataRow{metadata},
	}
	if err := p.queue.Publish(ctx, batch); err != nil {
		return fmt.Errorf("publish ingest batch %s: %w", page.URL, err)
	}
	return nil
}
