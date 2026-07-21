package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (d documentLineageEvictor) contentClusterDocumentRevision(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if revisions, ok := d.directory.(documentstore.DocumentRevisionDirectory); ok {
		document, found, err := revisions.DocumentRevision(ctx, normalizedURL)
		if err != nil {
			return documentstore.Document{}, false, fmt.Errorf(
				"read content cluster document revision: %w",
				err,
			)
		}

		return document, found, nil
	}

	document, found, err := d.directory.Document(ctx, normalizedURL)
	if err != nil {
		return documentstore.Document{}, false, fmt.Errorf("read content cluster document: %w", err)
	}

	return document, found, nil
}
