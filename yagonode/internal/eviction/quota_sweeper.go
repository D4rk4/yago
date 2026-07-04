package eviction

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yagonode/internal/urlreferences"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type quotaSweeper struct {
	vault      *vault.Vault
	postings   rwi.PostingPurger
	references urlreferences.ReferenceQuery
	urls       urlmeta.URLEvictor
	stale      urlmetastaleness.StaleURLSource
	target     float64
	batch      int
}

func (s quotaSweeper) Sweep(ctx context.Context) (Result, error) {
	quota := s.vault.QuotaBytes()
	if quota <= 0 || s.batch <= 0 {
		return Result{}, nil
	}
	highWater := int64(float64(quota) * s.target)

	var total Result
	for {
		used, err := s.vault.UsedBytes(ctx)
		if err != nil {
			return total, fmt.Errorf("measure usage: %w", err)
		}
		if used < highWater {
			return total, nil
		}

		candidates, err := s.stale.StalestURLs(ctx, s.batch)
		if err != nil {
			return total, fmt.Errorf("select stale urls: %w", err)
		}
		if len(candidates) == 0 {
			return total, nil
		}

		batch, err := s.purge(ctx, candidates)
		if err != nil {
			return total, err
		}
		total.URLsDeleted += batch.URLsDeleted
		total.PostingsDeleted += batch.PostingsDeleted
		if batch.URLsDeleted == 0 {
			return total, nil
		}
	}
}

func (s quotaSweeper) purge(ctx context.Context, urls []yagomodel.Hash) (Result, error) {
	return purgeURLs(ctx, s.vault, s.postings, s.references, s.urls, urls)
}

// purgeURLs drops the postings and metadata of the given URLs in one
// capacity-exempt transaction, so every collection clears atomically. It backs
// both the quota sweep and the on-demand Evictor.
//
//nolint:revive // each argument is a distinct collection the purge touches; bundling them would invent a hollow type
func purgeURLs(
	ctx context.Context,
	v *vault.Vault,
	postings rwi.PostingPurger,
	references urlreferences.ReferenceQuery,
	evictor urlmeta.URLEvictor,
	urls []yagomodel.Hash,
) (Result, error) {
	var result Result
	err := v.Update(ctx, func(tx *vault.Txn) error {
		for _, url := range urls {
			words, err := references.WordsReferencing(tx, url)
			if err != nil {
				return fmt.Errorf("words referencing url: %w", err)
			}
			for _, word := range words {
				deleted, err := postings.PurgePosting(tx, word, url)
				if err != nil {
					return fmt.Errorf("purge posting: %w", err)
				}
				if deleted {
					result.PostingsDeleted++
				}
			}
		}

		urlResult, err := evictor.Purge(ctx, tx, urls)
		if err != nil {
			return fmt.Errorf("purge urls: %w", err)
		}
		result.URLsDeleted = urlResult.URLsDeleted

		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("purge batch: %w", err)
	}

	return result, nil
}
