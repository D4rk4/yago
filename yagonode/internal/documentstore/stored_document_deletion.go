package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) deleteStoredDocument(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	location, found, shadowLegacy, err := d.locateStoredDocumentDeletion(ctx, normalizedURL)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if location.admission == 0 {
		return d.deleteLegacyStoredDocument(ctx, normalizedURL)
	}
	if err := d.deleteOrderedDocumentRows(
		ctx,
		normalizedURL,
		location.admission,
		shadowLegacy,
	); err != nil {
		return false, err
	}
	if err := d.deleteOrderedDocumentLocation(ctx, normalizedURL, location.admission); err != nil {
		return true, err
	}

	return true, nil
}

func (d documentVault) locateStoredDocumentDeletion(
	ctx context.Context,
	normalizedURL string,
) (storedDocumentLocation, bool, bool, error) {
	var location storedDocumentLocation
	var found bool
	var shadowLegacy bool
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		read, present, err := d.locateStoredDocument(tx, normalizedURL)
		location = read
		found = present
		shadowLegacy = location.admission > 0 &&
			d.legacyDocuments.Contains(tx, vault.Key(normalizedURL))

		return err
	})
	if err != nil {
		return storedDocumentLocation{}, false, false, fmt.Errorf(
			"locate deleted document: %w",
			err,
		)
	}

	return location, found, shadowLegacy, nil
}

func (d documentVault) deleteLegacyStoredDocument(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		_, err := d.legacyDocuments.Delete(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("remove legacy document: %w", err)
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("delete legacy document: %w", err)
	}

	return true, nil
}

func (d documentVault) deleteOrderedDocumentRows(
	ctx context.Context,
	normalizedURL string,
	admission uint64,
	shadowLegacy bool,
) error {
	key, err := orderedDocumentKey(admission, normalizedURL)
	if err != nil {
		return fmt.Errorf("resolve deleted document key: %w", err)
	}
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		current, present, err := d.documentLocations.Get(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("read deleted document location: %w", err)
		}
		if !present || current != admission {
			return fmt.Errorf("document location changed before deletion")
		}
		if _, err := d.orderedDocuments.Delete(tx, key); err != nil {
			return fmt.Errorf("remove ordered document row: %w", err)
		}
		if shadowLegacy {
			_, err = d.legacyDocuments.Delete(tx, vault.Key(normalizedURL))
			if err != nil {
				return fmt.Errorf("remove shadow legacy document: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("delete ordered document rows: %w", err)
	}

	return nil
}

func (d documentVault) deleteOrderedDocumentLocation(
	ctx context.Context,
	normalizedURL string,
	admission uint64,
) error {
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		current, present, err := d.documentLocations.Get(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("read hidden document location: %w", err)
		}
		if !present {
			return nil
		}
		if current != admission {
			return fmt.Errorf("document location changed before deletion")
		}
		_, err = d.documentLocations.Delete(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("remove ordered document location: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("delete ordered document location: %w", err)
	}

	return nil
}
