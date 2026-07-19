package frontier

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pendingPageBudgetCheckpoint interface {
	TrimPendingPages(context.Context, []byte, uint64) (uint64, error)
}

func (f *Frontier) enforceRecoveredRunPageBudget(
	ctx context.Context,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
	snapshot frontiercheckpoint.Snapshot,
	persistent bool,
) (frontiercheckpoint.Snapshot, error) {
	if !persistent || snapshot.Counters.Pending == 0 {
		return snapshot, nil
	}
	order := yagocrawlcontract.CrawlOrder{Priority: seed.Priority, Profile: profile.Profile}
	maximum := order.EffectiveMaxPagesPerRun(f.state.maxPagesPerRun)
	if maximum <= 0 {
		return snapshot, nil
	}
	if snapshot.Counters.Pages < snapshot.Counters.Pending {
		return frontiercheckpoint.Snapshot{}, fmt.Errorf(
			"%w: pending pages exceed admitted pages",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	completed := snapshot.Counters.Pages - snapshot.Counters.Pending -
		snapshot.BudgetDiscardedPages
	keep := uint64(0)
	if completed < uint64(maximum) {
		keep = min(snapshot.Counters.Pending, uint64(maximum)-completed)
	}
	if snapshot.Counters.Pending <= keep {
		return snapshot, nil
	}
	checkpoint, supported := f.checkpoint.(pendingPageBudgetCheckpoint)
	if !supported {
		return frontiercheckpoint.Snapshot{}, fmt.Errorf(
			"%w: pending page budget mutation is unavailable",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	if _, err := checkpoint.TrimPendingPages(
		context.WithoutCancel(ctx),
		seed.Provenance,
		keep,
	); err != nil {
		return frontiercheckpoint.Snapshot{}, fmt.Errorf("trim recovered crawl run budget: %w", err)
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

func (f *Frontier) reloadRecoveredRunSnapshot(
	ctx context.Context,
	provenance []byte,
) (frontiercheckpoint.Snapshot, error) {
	if bounded, supported := f.checkpoint.(boundedRecoveryCheckpoint); supported {
		snapshot, err := bounded.LoadBounded(
			context.WithoutCancel(ctx),
			provenance,
			frontierMutationBatchSize,
		)
		if err != nil {
			return frontiercheckpoint.Snapshot{}, fmt.Errorf(
				"reload bounded frontier checkpoint: %w",
				err,
			)
		}

		return snapshot, nil
	}
	snapshot, err := f.checkpoint.Load(context.WithoutCancel(ctx), provenance)
	if err != nil {
		return frontiercheckpoint.Snapshot{}, fmt.Errorf("reload frontier checkpoint: %w", err)
	}

	return snapshot, nil
}
