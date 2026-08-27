package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) DocumentsExist(
	ctx context.Context,
	normalizedURLs []string,
) ([]bool, error) {
	if len(normalizedURLs) == 0 {
		return []bool{}, nil
	}
	releaseURLs, err := d.urlBoundaries.lockReads(ctx, normalizedURLs)
	if err != nil {
		return nil, err
	}
	defer releaseURLs()
	found := make([]bool, len(normalizedURLs))
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		for index, normalizedURL := range normalizedURLs {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			location, present, err := d.locateStoredDocument(tx, normalizedURL)
			if err != nil {
				return err
			}
			if !present {
				continue
			}
			if location.admission == 0 {
				found[index] = true

				continue
			}
			key, err := orderedDocumentKey(location.admission, normalizedURL)
			if err != nil {
				return err
			}
			found[index] = d.orderedDocuments.Contains(tx, key)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("document presence: %w", err)
	}

	return found, nil
}
