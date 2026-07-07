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
	URLsDeleted     int
	PostingsDeleted int
}

type Sweeper interface {
	Sweep(ctx context.Context) (Result, error)
}

//nolint:revive // each argument is a distinct collection the sweep prunes; bundling them would invent a hollow type
func NewSweeper(
	vault *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	urls urlmeta.URLEvictor,
	stale urlmetastaleness.StaleURLSource,
	cfg Config,
) Sweeper {
	return quotaSweeper{
		vault:      vault,
		postings:   postings,
		references: references,
		urls:       urls,
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
}

func NewEvictor(
	vault *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	urls urlmeta.URLEvictor,
) Evictor {
	return Evictor{vault: vault, postings: postings, references: references, urls: urls}
}

// EvictURLs drops the postings and metadata of the given URL hashes.
func (e Evictor) EvictURLs(ctx context.Context, urls []yagomodel.Hash) (Result, error) {
	return purgeURLs(ctx, e.vault, e.postings, e.references, e.urls, urls)
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
