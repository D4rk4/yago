package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type pendingRunControlCompletion struct {
	LeaseID string
	Target  leaseControlTarget
}

func (q *DurableOrderQueue) pendingRunControlCompletions(
	ctx context.Context,
) ([]pendingRunControlCompletion, error) {
	pending := make([]pendingRunControlCompletion, 0)
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.leaseControlTargets.Scan(tx, nil, func(
			key vault.Key,
			target leaseControlTarget,
		) (bool, error) {
			pending = append(pending, pendingRunControlCompletion{
				LeaseID: string(key),
				Target:  target,
			})

			return true, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("read pending crawl run control completions: %w", err)
	}

	return pending, nil
}

func (q *DurableOrderQueue) completeRunControl(
	ctx context.Context,
	leaseID string,
) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		target, found, err := q.leaseControlTargets.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read crawl lease control target: %w", err)
		}
		if !found {
			return nil
		}
		settlement, retained, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read crawl lease settlement: %w", err)
		}
		if retained {
			if settlement.Outcome != leaseSettlementAcknowledged {
				return errLeaseDispositionConflict
			}
			if err := q.completedControlTargets.Put(tx, vault.Key(leaseID), target); err != nil {
				return fmt.Errorf("store completed crawl lease control target: %w", err)
			}
		}
		_, err = q.leaseControlTargets.Delete(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("delete crawl lease control target: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("complete crawl run control cleanup: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) replayRunControlCompletions(
	ctx context.Context,
	registry *ControlRegistry,
) error {
	pending, err := q.pendingRunControlCompletions(ctx)
	if err != nil {
		return err
	}
	for _, completion := range pending {
		if err := registry.CompleteRun(ctx, completion.Target); err != nil {
			return fmt.Errorf("replay crawl run control completion: %w", err)
		}
		if err := q.completeRunControl(ctx, completion.LeaseID); err != nil {
			return fmt.Errorf("replay crawl run control completion: %w", err)
		}
	}

	return nil
}
