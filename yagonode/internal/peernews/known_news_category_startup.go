package peernews

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) pruneKnownNewsCategories(ctx context.Context) error {
	after, err := p.cleanupCursor(ctx, categoryCleanupCursorKey)
	if err != nil {
		return fmt.Errorf("read known news category cleanup cursor: %w", err)
	}
	if after != nil {
		if err := p.validateKnownCategoryPrefix(ctx, after); err != nil {
			if !isStaleNewsCleanupCursor(err) {
				return fmt.Errorf("restore known news category cleanup: %w", err)
			}
			if err := p.clearCleanupCursor(ctx, categoryCleanupCursorKey); err != nil {
				return fmt.Errorf("reset known news category cleanup: %w", err)
			}
			after = nil
		}
	}
	for {
		progress, err := p.pruneKnownNewsCategoryPage(ctx, after)
		if err != nil {
			return fmt.Errorf("prune known news categories: %w", err)
		}
		if progress.after == nil {
			return nil
		}
		if err := p.storeCleanupCursor(ctx, categoryCleanupCursorKey, progress.after); err != nil {
			return fmt.Errorf("checkpoint known news category cleanup: %w", err)
		}
		if !progress.more {
			return nil
		}
		after = progress.after
	}
}

func (p *Pool) pruneKnownNewsCategoryPage(
	ctx context.Context,
	after vault.Key,
) (newsPruneProgress, error) {
	var progress newsPruneProgress
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		progress = newsPruneProgress{}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read known news category page: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(knownCategoryBucket, after, newsScrubPage)
		if err != nil {
			return fmt.Errorf("read known news category page: %w", err)
		}
		if len(page.Keys) == 0 {
			return nil
		}
		key := page.Keys[len(page.Keys)-1]
		progress = newsPruneProgress{
			after: append(vault.Key(nil), key...),
			more:  page.More,
		}
		for _, candidate := range page.Keys {
			if err := p.pruneKnownNewsCategoryRecord(tx, candidate); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return newsPruneProgress{}, fmt.Errorf("prune known news category page: %w", err)
	}

	return progress, nil
}
