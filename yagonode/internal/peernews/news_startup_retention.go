package peernews

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const newsScrubPage = 1024

type newsPruneProgress struct {
	after vault.Key
	more  bool
}

func (p *Pool) pruneKnownNews(ctx context.Context, now time.Time) error {
	newest := newBoundedNewestNews(p.retention.knownRecords, -1)
	after, err := p.cleanupCursor(ctx, knownCleanupCursorKey)
	if err != nil {
		return fmt.Errorf("read known news cleanup cursor: %w", err)
	}
	if after != nil {
		if err := p.restoreKnownNewsPrefix(ctx, now, after, newest); err != nil {
			if !isStaleNewsCleanupCursor(err) {
				return fmt.Errorf("restore known news cleanup: %w", err)
			}
			if err := p.clearCleanupCursor(ctx, knownCleanupCursorKey); err != nil {
				return fmt.Errorf("reset known news cleanup: %w", err)
			}
			after = nil
			newest = newBoundedNewestNews(p.retention.knownRecords, -1)
		}
	}
	for {
		progress, err := p.pruneKnownNewsPage(ctx, now, after, newest)
		if err != nil {
			return fmt.Errorf("prune known news: %w", err)
		}
		if progress.after == nil {
			return nil
		}
		if err := p.storeCleanupCursor(ctx, knownCleanupCursorKey, progress.after); err != nil {
			return fmt.Errorf("checkpoint known news cleanup: %w", err)
		}
		if !progress.more {
			return nil
		}
		after = progress.after
	}
}

func (p *Pool) pruneQueuedNews(ctx context.Context, now time.Time) error {
	catalog, err := p.buildQueuedNewsEvidenceCatalog(ctx, now)
	if err != nil {
		return fmt.Errorf("catalog queued news: %w", err)
	}
	if err := p.raiseQueuedNewsCursors(ctx, catalog.latestSequence); err != nil {
		return fmt.Errorf("reconcile queued news cursors: %w", err)
	}
	after, newest, err := p.queuedNewsCleanupStart(ctx, now, catalog.evidence)
	if err != nil {
		return err
	}
	for {
		progress, err := p.pruneQueuedNewsPage(ctx, now, after, newest, catalog.evidence)
		if err != nil {
			return fmt.Errorf("prune queued news: %w", err)
		}
		if progress.after == nil {
			return nil
		}
		if err := p.storeCleanupCursor(ctx, queuedCleanupCursorKey, progress.after); err != nil {
			return fmt.Errorf("checkpoint queued news cleanup: %w", err)
		}
		if !progress.more {
			return nil
		}
		after = progress.after
	}
}

func (p *Pool) queuedNewsCleanupStart(
	ctx context.Context,
	now time.Time,
	catalog queuedNewsEvidenceCatalog,
) (vault.Key, *boundedNewestNews, error) {
	newest := newBoundedNewestNews(p.retention.queueRecords, p.retention.queueBytes)
	after, err := p.cleanupCursor(ctx, queuedCleanupCursorKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read queued news cleanup cursor: %w", err)
	}
	if after == nil {
		return nil, newest, nil
	}
	restoreErr := p.restoreQueuedNewsPrefix(ctx, now, after, newest, catalog)
	if restoreErr == nil {
		return after, newest, nil
	}
	if !isStaleNewsCleanupCursor(restoreErr) {
		return nil, nil, fmt.Errorf("restore queued news cleanup: %w", restoreErr)
	}
	if err := p.clearCleanupCursor(ctx, queuedCleanupCursorKey); err != nil {
		return nil, nil, fmt.Errorf("reset queued news cleanup: %w", err)
	}

	return nil, newBoundedNewestNews(
		p.retention.queueRecords,
		p.retention.queueBytes,
	), nil
}

func (p *Pool) deleteInvalidKnownNews(tx *vault.Txn, key vault.Key) error {
	if err := p.forgetKnownNews(tx, key); err != nil {
		return fmt.Errorf("evict invalid known news: %w", err)
	}

	return nil
}

func (p *Pool) deleteInvalidQueuedNews(tx *vault.Txn, key vault.Key) error {
	_, err := p.queue.Delete(tx, key)
	if err != nil {
		return fmt.Errorf("evict invalid queued news: %w", err)
	}

	return nil
}
