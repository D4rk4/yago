package documentstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) Documents(
	ctx context.Context,
	normalizedURLs []string,
) ([]Document, []bool, error) {
	if len(normalizedURLs) == 0 {
		return []Document{}, []bool{}, nil
	}
	releaseURLs, err := d.urlBoundaries.lockReads(ctx, normalizedURLs)
	if err != nil {
		return nil, nil, err
	}
	defer releaseURLs()
	var documents []Document
	var found []bool
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		var readErr error
		documents, found, readErr = d.readStoredDocumentSet(
			ctx,
			tx,
			normalizedURLs,
		)

		return readErr
	})
	if err != nil {
		return nil, nil, fmt.Errorf("documents: %w", err)
	}

	return documents, found, nil
}

func (d documentVault) readStoredDocumentSet(
	ctx context.Context,
	tx *vault.Txn,
	normalizedURLs []string,
) ([]Document, []bool, error) {
	keys := documentPresenceKeys(normalizedURLs)
	admissions, located, err := d.documentLocations.Values(ctx, tx, keys)
	if err != nil {
		return nil, nil, fmt.Errorf("read document locations: %w", err)
	}
	selection, err := selectStoredDocumentPresence(
		normalizedURLs,
		keys,
		admissions,
		located,
	)
	if err != nil {
		return nil, nil, err
	}
	ordered, orderedFound, err := d.orderedDocuments.Values(
		ctx,
		tx,
		selection.orderedKeys,
	)
	if err != nil {
		return d.readStoredDocumentSetAfterBatchFailure(
			ctx,
			tx,
			normalizedURLs,
			"read ordered documents",
			err,
		)
	}
	legacy, legacyFound, err := d.legacyDocuments.Values(
		ctx,
		tx,
		selection.legacyKeys,
	)
	if err != nil {
		return d.readStoredDocumentSetAfterBatchFailure(
			ctx,
			tx,
			normalizedURLs,
			"read legacy documents",
			err,
		)
	}
	read := storedDocumentSetRead{
		documents: make([]Document, len(normalizedURLs)),
		found:     make([]bool, len(normalizedURLs)),
	}
	selection.recordDocuments(
		normalizedURLs,
		&read,
		storedDocumentSetRead{documents: ordered, found: orderedFound},
		storedDocumentSetRead{documents: legacy, found: legacyFound},
	)

	return read.documents, read.found, nil
}

func (d documentVault) readStoredDocumentSetAfterBatchFailure(
	ctx context.Context,
	tx *vault.Txn,
	normalizedURLs []string,
	operation string,
	failure error,
) ([]Document, []bool, error) {
	if !errors.Is(failure, vault.ErrCorruptValue) {
		return nil, nil, fmt.Errorf("%s: %w", operation, failure)
	}
	documents := make([]Document, len(normalizedURLs))
	found := make([]bool, len(normalizedURLs))
	for index, normalizedURL := range normalizedURLs {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("context: %w", err)
		}
		document, _, present, err := d.readStoredDocument(tx, normalizedURL)
		if err != nil {
			return nil, nil, err
		}
		documents[index] = document
		found[index] = present
	}

	return documents, found, nil
}
