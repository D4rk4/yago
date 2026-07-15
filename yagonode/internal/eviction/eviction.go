// Package eviction frees storage when the vault nears its quota. It owns no
// buckets: it reads usage from the storage kernel, names the stalest URLs, looks
// up the words referencing each, and drops their postings and metadata within one
// capacity-exempt transaction, so every collection clears atomically without
// sharing a schema.
package eviction

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type Config struct {
	TargetFraction float64
	BatchSize      int
}

type Result struct {
	URLsDeleted      int
	PostingsDeleted  int
	DocumentsDeleted int
}

type Sweeper interface {
	Sweep(ctx context.Context) (Result, error)
}

// DocumentEvictor drops a stored document by its normalized URL. Documents are
// keyed by the URL string, not the URL hash the postings and metadata use, so a
// purge resolves the hash to its URL (URLResolver) before deleting the document
type DocumentEvictor interface {
	Delete(ctx context.Context, normalizedURL string) (bool, error)
}

type ReservedDocumentEviction interface {
	Delete(context.Context, string) (bool, error)
	Release()
}

type documentEvictionReserver interface {
	ReserveDocumentEvictions(
		context.Context,
		[]string,
	) (ReservedDocumentEviction, error)
}

// URLResolver recovers the URL a document is keyed by from a URL hash, by
// reading the url-metadata row (whose "url" property is the wire-encoded URL).
type URLResolver interface {
	RowsByHash(ctx context.Context, hashes []yagomodel.Hash) ([]yagomodel.URIMetadataRow, error)
}

//nolint:revive // each argument is a distinct collection the sweep prunes; bundling them would invent a hollow type
func NewSweeper(
	vault *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	urls urlmeta.URLEvictor,
	documents DocumentEvictor,
	resolver URLResolver,
	stale urlmetastaleness.StaleURLSource,
	cfg Config,
) Sweeper {
	return quotaSweeper{
		vault:      vault,
		postings:   postings,
		references: references,
		urls:       urls,
		documents:  documents,
		resolver:   resolver,
		stale:      stale,
		target:     cfg.TargetFraction,
		batch:      cfg.BatchSize,
	}
}

// Evictor removes specific URLs on demand — the operator-driven counterpart to the
// quota Sweep — running the same atomic posting-and-metadata purge.
type Evictor struct {
	vault      *vault.Vault
	postings   rwi.PostingPurger
	references urlreferences.ReferenceQuery
	urls       urlmeta.URLEvictor
	documents  DocumentEvictor
	resolver   URLResolver
}

//nolint:revive // each argument is a distinct collection the evictor prunes; bundling them would invent a hollow type
func NewEvictor(
	vault *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	urls urlmeta.URLEvictor,
	documents DocumentEvictor,
	resolver URLResolver,
) Evictor {
	return Evictor{
		vault:      vault,
		postings:   postings,
		references: references,
		urls:       urls,
		documents:  documents,
		resolver:   resolver,
	}
}

// EvictURLs drops the postings, metadata, and document of the given URL hashes.
func (e Evictor) EvictURLs(ctx context.Context, urls []yagomodel.Hash) (Result, error) {
	return purgeURLs(ctx, e.vault, e.postings, e.references, e.urls, e.documents, e.resolver, urls)
}

// Purge drops the given URL hashes and reports only whether the drop succeeded,
// discarding the deletion counts. It is the dead-page tombstone port (ADR-0034):
// a recrawl found a URL permanently gone and only needs it purged, idempotently.
func (e Evictor) Purge(ctx context.Context, urls []yagomodel.Hash) error {
	if _, err := e.EvictURLs(ctx, urls); err != nil {
		return err
	}

	return nil
}

func (e Evictor) PurgeResolved(
	ctx context.Context,
	normalizedURLs []string,
	urls []yagomodel.Hash,
) error {
	deletion := resolvedURLLineageDeletion{
		vault:     e.vault,
		documents: e.documents,
		projections: urlProjectionDeletion{
			postings:   e.postings,
			references: e.references,
			metadata:   e.urls,
		},
	}
	if _, err := deletion.purge(
		ctx,
		normalizedURLs,
		urls,
	); err != nil {
		return err
	}

	return nil
}
