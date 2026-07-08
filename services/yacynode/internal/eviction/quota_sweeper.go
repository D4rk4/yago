package eviction

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmetastaleness"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlreferences"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
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

func (s quotaSweeper) purge(ctx context.Context, urls []yacymodel.Hash) (Result, error) {
	var result Result
	err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, url := range urls {
			words, err := s.references.WordsReferencing(tx, url)
			if err != nil {
				return fmt.Errorf("words referencing url: %w", err)
			}
			for _, word := range words {
				deleted, err := s.postings.PurgePosting(tx, word, url)
				if err != nil {
					return fmt.Errorf("purge posting: %w", err)
				}
				if deleted {
					result.PostingsDeleted++
				}
			}
		}

		urlResult, err := s.urls.Purge(ctx, tx, urls)
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
