package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) canonicalDocuments(
	ctx context.Context,
	docs []documentstore.Document,
) ([]documentstore.Document, error) {
	directory, ok := c.documents.(documentstore.CanonicalDocumentDirectory)
	if !ok {
		return docs, nil
	}

	canonical, err := directory.CanonicalDocuments(ctx, docs)
	if err != nil {
		return nil, fmt.Errorf("canonicalize documents: %w", err)
	}

	return canonical, nil
}

func (c *IngestConsumer) committedDocuments(
	receipt documentstore.Receipt,
	fallback []documentstore.Document,
) []documentstore.Document {
	if len(receipt.CommittedDocuments) > 0 {
		return receipt.CommittedDocuments
	}
	if _, ok := c.documents.(documentstore.CanonicalDocumentDirectory); ok {
		return nil
	}

	return fallback
}
