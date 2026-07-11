package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maxExtractedTextBytes = 1 << 20

type documentVault struct {
	vault           *vault.Vault
	collection      *vault.Collection[Document]
	inboundAnchors  *vault.Collection[[]AnchorText]
	outboundTargets *vault.Collection[[]string]
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

	doc, accepted, found, err := d.canonicalDocument(tx, doc)
	if err != nil {
		return err
	}
	if !accepted {
		receipt.Rejected++
		return nil
	}

	key := vault.Key(doc.NormalizedURL)
	if err := d.collection.Put(tx, key, doc); err != nil {
		return fmt.Errorf("store document: %w", err)
	}
	if found {
		receipt.Updated++
	} else {
		receipt.Stored++
	}
	receipt.CommittedDocuments = append(receipt.CommittedDocuments, doc)

	return nil
}

func (d documentVault) CanonicalDocuments(
	ctx context.Context,
	docs []Document,
) ([]Document, error) {
	canonical := make([]Document, 0, len(docs))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, doc := range docs {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			prepared, accepted, _, err := d.canonicalDocument(tx, doc)
			if err != nil {
				return err
			}
			if accepted {
				canonical = append(canonical, prepared)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("canonical documents: %w", err)
	}

	return canonical, nil
}

func (d documentVault) canonicalDocument(
	tx *vault.Txn,
	doc Document,
) (Document, bool, bool, error) {
	doc = normalizedDocument(doc)
	if doc.NormalizedURL == "" {
		return Document{}, false, false, nil
	}

	key := vault.Key(doc.NormalizedURL)
	previous, found, err := d.collection.Get(tx, key)
	if err != nil {
		return Document{}, false, false, fmt.Errorf("read document: %w", err)
	}
	doc = mergeDocumentDates(previous, doc, found)
	storedAnchors, anchorsFound, err := d.inboundAnchors.Get(tx, key)
	if err != nil {
		return Document{}, false, false, fmt.Errorf("read inbound anchors: %w", err)
	}
	if anchorsFound {
		doc.Inlinks = canonicalAnchorTexts(append(doc.Inlinks, storedAnchors...))
	}

	return doc, true, found, nil
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

func (d documentVault) Delete(ctx context.Context, normalizedURL string) (bool, error) {
	var removed bool
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		deleted, err := d.collection.Delete(tx, vault.Key(normalizedURL))
		if err != nil {
			return fmt.Errorf("delete document: %w", err)
		}
		removed = deleted

		return nil
	}); err != nil {
		return false, fmt.Errorf("delete document: %w", err)
	}

	return removed, nil
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
	doc.ContentSafety = normalizedContentSafetyEvidence(doc.ContentSafety)
	doc.Headings = append([]string(nil), doc.Headings...)
	doc.Outlinks = append([]string(nil), doc.Outlinks...)
	doc.Inlinks = append([]AnchorText(nil), doc.Inlinks...)
	doc.OutboundAnchors = append([]OutboundAnchor(nil), doc.OutboundAnchors...)
	doc.Images = append([]ImageMetadata(nil), doc.Images...)
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
	_ DocumentDirectory          = documentVault{}
	_ CanonicalDocumentDirectory = documentVault{}
	_ DocumentReceiver           = documentVault{}
	_ StoredDocuments            = documentVault{}
	_ InboundAnchorReceiver      = documentVault{}
)
