package documentstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) recoverFailedDocumentPublication(
	ctx context.Context,
	locations map[string]storedDocumentLocationPublication,
	publicationError error,
) error {
	cleanupError := d.deleteUnpublishedDocumentRows(ctx, locations)
	if cleanupError == nil {
		return publicationError
	}

	return errors.Join(publicationError, cleanupError)
}

func (d documentVault) deleteUnpublishedDocumentRows(
	ctx context.Context,
	locations map[string]storedDocumentLocationPublication,
) error {
	urls := slices.Sorted(maps.Keys(locations))
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, normalizedURL := range urls {
			publication := locations[normalizedURL]
			current, located, err := d.documentLocations.Get(
				tx,
				vault.Key(normalizedURL),
			)
			if err != nil {
				return fmt.Errorf("read failed document publication: %w", err)
			}
			if located && current == publication.admission {
				continue
			}
			key, err := orderedDocumentKey(publication.admission, normalizedURL)
			if err != nil {
				return fmt.Errorf("resolve unpublished document row: %w", err)
			}
			if _, err := d.orderedDocuments.Delete(tx, key); err != nil {
				return fmt.Errorf("delete unpublished document row: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("clean failed document publication: %w", err)
	}

	return nil
}
