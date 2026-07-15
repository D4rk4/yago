package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

func (c *indexAdminController) deleteResolvedOne(
	ctx context.Context,
	normalizedURL string,
	resolved resolvedURLEvictor,
) error {
	_, completeLineage := c.documents.(reservedDocumentEvictor)
	hash, err := c.hashURL(normalizedURL)
	if err != nil {
		slog.WarnContext(ctx, "derive url hash for eviction failed",
			slog.String("url", normalizedURL), slog.Any("error", err))

		return c.deleteUnhashableResolvedOne(ctx, normalizedURL, completeLineage)
	}
	if !completeLineage {
		if err := c.index.Delete(ctx, normalizedURL); err != nil {
			return fmt.Errorf("delete from search index: %w", err)
		}
	}
	if err := resolved.PurgeResolved(
		ctx,
		[]string{normalizedURL},
		[]yagomodel.Hash{hash.Hash()},
	); err != nil {
		return fmt.Errorf("evict document lineage: %w", err)
	}

	return nil
}

func (c *indexAdminController) deleteUnhashableResolvedOne(
	ctx context.Context,
	normalizedURL string,
	completeLineage bool,
) error {
	if !completeLineage {
		if err := c.index.Delete(ctx, normalizedURL); err != nil {
			return fmt.Errorf("delete from search index: %w", err)
		}
	}
	if c.documents != nil {
		if _, err := c.documents.Delete(ctx, normalizedURL); err != nil {
			return fmt.Errorf("delete document: %w", err)
		}
	}

	return nil
}
