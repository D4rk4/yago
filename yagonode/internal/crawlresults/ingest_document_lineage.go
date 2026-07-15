package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) canonicalIngestDocuments(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	documents []documentstore.Document,
) ([]documentstore.Document, error) {
	if reservation == nil {
		return c.canonicalDocuments(ctx, documents)
	}
	if c.reservedDocuments == nil {
		return nil, fmt.Errorf("reserved canonical document directory is unavailable")
	}

	canonical, err := c.reservedDocuments.CanonicalReservedDocuments(
		ctx,
		reservation,
		documents,
	)
	if err != nil {
		return nil, fmt.Errorf("canonicalize reserved documents: %w", err)
	}

	return canonical, nil
}

func (c *IngestConsumer) reserveIngestDocumentLineages(
	ctx context.Context,
	deliveries []IngestDelivery,
) (documentstore.DocumentLineageReservation, error) {
	if c.lineages == nil {
		return nil, nil
	}
	urls := make([]string, 0, len(deliveries)*3)
	for _, delivery := range deliveries {
		urls = append(urls, delivery.Batch.SourceURL)
		if hasDocument(delivery.Batch.Document) {
			urls = append(
				urls,
				delivery.Batch.Document.NormalizedURL,
				delivery.Batch.Document.CanonicalURL,
			)
		}
	}

	reservation, err := c.lineages.ReserveDocumentLineages(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("reserve ingest document lineages: %w", err)
	}

	return reservation, nil
}

func (c *IngestConsumer) releaseIngestDocumentLineages(
	reservation documentstore.DocumentLineageReservation,
) {
	if c.lineages != nil && reservation != nil {
		c.lineages.ReleaseDocumentLineages(reservation)
	}
}
