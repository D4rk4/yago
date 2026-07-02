package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const maxExtractedTextBytes = 1 << 20

type documentVault struct {
	vault      *vault.Vault
	collection *vault.Collection[Document]
}

func (d documentVault) Receive(ctx context.Context, docs []Document) (Receipt, error) {
	if len(docs) == 0 {
		return Receipt{}, nil
	}

	atCapacity, err := d.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
	}

	receipt, err := d.store(ctx, docs)
	if err != nil {
		return Receipt{}, err
	}

	return receipt, nil
}

func (d documentVault) store(ctx context.Context, docs []Document) (Receipt, error) {
	var receipt Receipt
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, doc := range docs {
			if err := d.storeOne(ctx, tx, doc, &receipt); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return Receipt{}, fmt.Errorf("store documents: %w", err)
	}

	return receipt, nil
}

func (d documentVault) storeOne(
	ctx context.Context,
	tx *vault.Txn,
	doc Document,
	receipt *Receipt,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	doc = normalizedDocument(doc)
	if doc.NormalizedURL == "" {
		receipt.Rejected++
		return nil
	}

	key := vault.Key(doc.NormalizedURL)
	_, found, err := d.collection.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read document: %w", err)
	}
	if err := d.collection.Put(tx, key, doc); err != nil {
		return fmt.Errorf("store document: %w", err)
	}
	if found {
		receipt.Updated++
	} else {
		receipt.Stored++
	}

	return nil
}

func (d documentVault) Document(
	ctx context.Context,
	normalizedURL string,
) (Document, bool, error) {
	var doc Document
	var found bool
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := d.collection.Get(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("read document: %w", err)
		}

		doc = got
		found = ok
		return nil
	})
	if err != nil {
		return Document{}, false, fmt.Errorf("document: %w", err)
	}

	return doc, found, nil
}

func (d documentVault) Count(ctx context.Context) (int, error) {
	var count int
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		length, err := d.collection.Len(tx)
		if err != nil {
			return fmt.Errorf("read document length: %w", err)
		}
		count = length

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("document count: %w", err)
	}

	return count, nil
}

func (d documentVault) StoredDocuments(
	ctx context.Context,
	visit func(Document) (bool, error),
) error {
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		return d.collection.Scan(
			tx,
			nil,
			func(_ vault.Key, doc Document) (bool, error) {
				if err := ctx.Err(); err != nil {
					return false, fmt.Errorf("context: %w", err)
				}

				return visit(doc)
			},
		)
	})
	if err != nil {
		return fmt.Errorf("stored documents: %w", err)
	}

	return nil
}

func normalizedDocument(doc Document) Document {
	if doc.NormalizedURL == "" {
		doc.NormalizedURL = doc.CanonicalURL
	}
	if doc.CanonicalURL == "" {
		doc.CanonicalURL = doc.NormalizedURL
	}
	doc.ExtractedText = boundedText(doc.ExtractedText)
	doc.Headings = append([]string(nil), doc.Headings...)
	doc.Outlinks = append([]string(nil), doc.Outlinks...)
	doc.Inlinks = append([]AnchorText(nil), doc.Inlinks...)
	if doc.Metadata != nil {
		metadata := make(map[string]string, len(doc.Metadata))
		for key, value := range doc.Metadata {
			metadata[key] = value
		}
		doc.Metadata = metadata
	}

	return doc
}

func boundedText(text string) string {
	if len(text) <= maxExtractedTextBytes {
		return text
	}

	end := 0
	for index := range text {
		if index > maxExtractedTextBytes {
			break
		}
		end = index
	}
	return text[:end]
}

var (
	_ DocumentDirectory = documentVault{}
	_ DocumentReceiver  = documentVault{}
	_ StoredDocuments   = documentVault{}
)
