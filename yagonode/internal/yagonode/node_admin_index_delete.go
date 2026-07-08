package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// searchIndexDeleter removes a document from the full-text index by its ID.
type searchIndexDeleter interface {
	Delete(ctx context.Context, docID string) error
}

// urlEvictor drops a URL's postings and metadata on demand.
type urlEvictor interface {
	EvictURLs(ctx context.Context, urls []yagomodel.Hash) (eviction.Result, error)
}

// indexAdminController removes indexed documents from every store lineage they
// participate in: the full-text index (by normalized URL), the document store,
// and the RWI postings + URL metadata (keyed by the document's URL hash).
type indexAdminController struct {
	index     searchIndexDeleter
	documents documentstore.DocumentEvictor
	stored    documentstore.StoredDocuments
	evictor   urlEvictor
	hashURL   func(string) (yagomodel.URLHash, error)
}

func newIndexAdminController(storage nodeStorage, v *vault.Vault) *indexAdminController {
	deleter, _ := storage.documentDirectory.(documentstore.DocumentEvictor)

	return &indexAdminController{
		index:     storage.searchIndex,
		documents: deleter,
		stored:    storage.storedDocuments(),
		evictor: eviction.NewEvictor(
			// Documents are dropped explicitly by deleteOne here, so the evictor
			// skips the documents side (nil) to avoid a redundant second delete.
			v, storage.postingPurger, storage.references, storage.urlEvictor,
			nil, nil,
		),
		hashURL: yagomodel.HashURL,
	}
}

// DeleteDocument removes a single document by its store key (normalized URL).
func (c *indexAdminController) DeleteDocument(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}

	return c.deleteOne(ctx, key)
}

// DeleteDomain removes every stored document whose host is the given domain or a
// subdomain of it, returning how many were deleted.
func (c *indexAdminController) DeleteDomain(ctx context.Context, domain string) (int, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return 0, nil
	}

	var keys []string
	if err := c.stored.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		if doc.NormalizedURL != "" && documentDomainMatches(documentURL(doc), domain) {
			keys = append(keys, doc.NormalizedURL)
		}

		return true, nil
	}); err != nil {
		return 0, fmt.Errorf("enumerate documents: %w", err)
	}

	deleted := 0
	for _, key := range keys {
		if err := c.deleteOne(ctx, key); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}

func (c *indexAdminController) deleteOne(ctx context.Context, normalizedURL string) error {
	if err := c.index.Delete(ctx, normalizedURL); err != nil {
		return fmt.Errorf("delete from search index: %w", err)
	}
	if c.documents != nil {
		if _, err := c.documents.Delete(ctx, normalizedURL); err != nil {
			return fmt.Errorf("delete document: %w", err)
		}
	}

	hash, err := c.hashURL(normalizedURL)
	if err != nil {
		slog.WarnContext(ctx, "derive url hash for eviction failed",
			slog.String("url", normalizedURL), slog.Any("error", err))

		return nil
	}
	if _, err := c.evictor.EvictURLs(ctx, []yagomodel.Hash{hash.Hash()}); err != nil {
		return fmt.Errorf("evict postings: %w", err)
	}

	return nil
}
