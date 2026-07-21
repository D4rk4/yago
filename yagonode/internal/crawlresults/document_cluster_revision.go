package crawlresults

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (c *IngestConsumer) documentClusterRevision(
	ctx context.Context,
	directory documentstore.DocumentDirectory,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if revisions, ok := c.documents.(documentstore.DocumentRevisionDirectory); ok {
		document, found, err := revisions.DocumentRevision(ctx, normalizedURL)
		if err != nil {
			return documentstore.Document{}, false, fmt.Errorf("read document revision: %w", err)
		}

		return document, found, nil
	}

	document, found, err := directory.Document(ctx, normalizedURL)
	if err != nil {
		return documentstore.Document{}, false, fmt.Errorf("read document: %w", err)
	}

	return document, found, nil
}
