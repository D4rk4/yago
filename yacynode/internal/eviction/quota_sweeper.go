package eviction

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type quotaSweeper struct {
	vault    *boltvault.Vault
	postings rwi.PostingDirectory
	urls     urlmeta.URLEvictor
	stale    StaleURLSource
	target   float64
	batch    int
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
	err := s.vault.Update(ctx, func(tx *boltvault.Txn) error {
		postingResult, err := s.postings.PurgeReferences(tx, urls)
		if err != nil {
			return fmt.Errorf("purge references: %w", err)
		}
		urlResult, err := s.urls.Purge(ctx, tx, urls)
		if err != nil {
			return fmt.Errorf("purge urls: %w", err)
		}
		result = Result{
			URLsDeleted:     urlResult.URLsDeleted,
			PostingsDeleted: postingResult.PostingsDeleted,
		}

		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("purge batch: %w", err)
	}

	return result, nil
}
