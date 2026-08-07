package frontier

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type pendingPageDepthCheckpoint interface {
	DiscardPendingPagesBeyondDepth(context.Context, []byte, int) (uint64, error)
}

func (f *Frontier) enforceRecoveredRunDepth(
	ctx context.Context,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
	snapshot frontiercheckpoint.Snapshot,
	persistent bool,
) (frontiercheckpoint.Snapshot, error) {
	if !persistent || snapshot.Counters.Pending == 0 {
		return snapshot, nil
	}
	checkpoint, supported := f.checkpoint.(pendingPageDepthCheckpoint)
	if !supported {
		for _, page := range snapshot.Outstanding {
			if page.Depth > profile.Profile.MaxDepth {
				return frontiercheckpoint.Snapshot{}, fmt.Errorf(
					"%w: pending page depth mutation is unavailable",
					frontiercheckpoint.ErrCorruptCheckpoint,
				)
			}
		}

		return snapshot, nil
	}
	discarded, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.WithoutCancel(ctx),
		seed.Provenance,
		profile.Profile.MaxDepth,
	)
	if err != nil {
		return frontiercheckpoint.Snapshot{}, fmt.Errorf(
			"discard recovered crawl pages beyond current depth: %w",
			err,
		)
	}
	if discarded == 0 {
		return snapshot, nil
	}
	reloaded, err := f.reloadRecoveredRunSnapshot(ctx, seed.Provenance)
	if err != nil {
		return frontiercheckpoint.Snapshot{}, err
	}
	if err := validateCheckpointSnapshot(
		reloaded,
		seed,
		normalizeCrawlOrderPriority(seed.Priority),
		profile.Profile.Handle,
	); err != nil {
		return frontiercheckpoint.Snapshot{}, err
	}

	return reloaded, nil
}
