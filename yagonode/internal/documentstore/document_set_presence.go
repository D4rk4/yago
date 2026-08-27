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
		keys := documentPresenceKeys(normalizedURLs)
		admissions, located, err := d.documentLocations.Values(ctx, tx, keys)
		if err != nil {
			return fmt.Errorf("read document locations: %w", err)
		}
		selection, err := selectStoredDocumentPresence(
			normalizedURLs,
			keys,
			admissions,
			located,
		)
		if err != nil {
			return err
		}
		orderedPresence, err := d.orderedDocuments.Presence(ctx, tx, selection.orderedKeys)
		if err != nil {
			return fmt.Errorf("read ordered document presence: %w", err)
		}
		legacyPresence, err := d.legacyDocuments.Presence(ctx, tx, selection.legacyKeys)
		if err != nil {
			return fmt.Errorf("read legacy document presence: %w", err)
		}
		selection.record(found, orderedPresence, legacyPresence)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("document presence: %w", err)
	}

	return found, nil
}
