package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func scanCorpusSignalDocuments(
	ctx context.Context,
	documents documentstore.StoredDocuments,
	visit func(documentstore.Document) (bool, error),
) error {
	if paged, ok := documents.(documentstore.StoredDocumentPageScanner); ok {
		if err := paged.ScanStoredDocumentPages(
			ctx,
			visit,
		); err != nil {
			return fmt.Errorf("scan paged corpus documents: %w", err)
		}

		return nil
	}
	if err := documents.StoredDocuments(ctx, visit); err != nil {
		return fmt.Errorf("scan corpus documents: %w", err)
	}

	return nil
}
