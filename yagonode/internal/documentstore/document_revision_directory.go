package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type DocumentRevisionDirectory interface {
	DocumentRevision(context.Context, string) (Document, bool, error)
}

func (d documentVault) DocumentRevision(
	ctx context.Context,
	normalizedURL string,
) (Document, bool, error) {
	releaseURL, err := d.urlBoundaries.lockReads(ctx, []string{normalizedURL})
	if err != nil {
		return Document{}, false, err
	}
	defer releaseURL()
	var document Document
	var found bool
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		read, _, present, err := d.readStoredDocument(tx, normalizedURL)
		if err != nil || !present {
			document = read
			found = present

			return err
		}
		materialized, _, err := d.inboundAnchors.Get(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("read document revision inbound anchors: %w", err)
		}
		document = retainDocumentRevisionInlinks(read, materialized)
		found = true

		return nil
	})
	if err != nil {
		return Document{}, false, fmt.Errorf("document revision: %w", err)
	}

	return document, found, nil
}

var _ DocumentRevisionDirectory = documentVault{}
