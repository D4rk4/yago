package documentstore

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type storedDocumentLocation struct {
	admission uint64
}

func (d documentVault) locateStoredDocument(
	tx *vault.Txn,
	normalizedURL string,
) (storedDocumentLocation, bool, error) {
	admission, located, err := d.documentLocations.Get(
		tx,
		vault.Key(normalizedURL),
	)
	if err != nil {
		return storedDocumentLocation{}, false, fmt.Errorf(
			"read document location: %w",
			err,
		)
	}
	if located {
		return storedDocumentLocation{admission: admission}, true, nil
	}
	if d.legacyDocuments.Contains(tx, vault.Key(normalizedURL)) {
		return storedDocumentLocation{}, true, nil
	}

	return storedDocumentLocation{}, false, nil
}

func (d documentVault) readStoredDocument(
	tx *vault.Txn,
	normalizedURL string,
) (Document, storedDocumentLocation, bool, error) {
	location, found, err := d.locateStoredDocument(tx, normalizedURL)
	if err != nil || !found {
		return Document{}, storedDocumentLocation{}, found, err
	}
	if location.admission == 0 {
		document, present, err := decodedStoredDocument(d.legacyDocuments.Get(
			tx,
			vault.Key(normalizedURL),
		))
		if err != nil {
			return Document{}, location, false, fmt.Errorf(
				"read legacy stored document: %w",
				err,
			)
		}
		if present && document.NormalizedURL != normalizedURL {
			return Document{}, location, false, nil
		}

		return document, location, present, nil
	}
	key, err := orderedDocumentKey(location.admission, normalizedURL)
	if err != nil {
		return Document{}, storedDocumentLocation{}, false, fmt.Errorf(
			"resolve ordered document key: %w",
			err,
		)
	}
	document, present, err := decodedStoredDocument(d.orderedDocuments.Get(tx, key))
	if err != nil {
		return Document{}, location, false, fmt.Errorf(
			"read ordered stored document: %w",
			err,
		)
	}
	if !present {
		return Document{}, location, false, nil
	}
	if document.NormalizedURL != normalizedURL {
		return Document{}, location, false, nil
	}

	return document, location, true, nil
}

func decodedStoredDocument(
	document Document,
	present bool,
	err error,
) (Document, bool, error) {
	if errors.Is(err, vault.ErrCorruptValue) {
		return Document{}, false, nil
	}

	return document, present, err
}

func (d documentVault) putStoredDocument(
	tx *vault.Txn,
	location storedDocumentLocation,
	document Document,
) error {
	if location.admission == 0 {
		if err := d.legacyDocuments.Put(
			tx,
			vault.Key(document.NormalizedURL),
			document,
		); err != nil {
			return fmt.Errorf("store legacy document: %w", err)
		}

		return nil
	}
	key, err := orderedDocumentKey(location.admission, document.NormalizedURL)
	if err != nil {
		return fmt.Errorf("resolve ordered document key: %w", err)
	}
	if err := d.orderedDocuments.Put(tx, key, document); err != nil {
		return fmt.Errorf("store ordered document: %w", err)
	}

	return nil
}
