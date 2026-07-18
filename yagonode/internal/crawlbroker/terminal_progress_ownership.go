package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) terminalProgressOwnedBy(
	ctx context.Context,
	workerID string,
	runID string,
) (bool, error) {
	if workerID == "" || runID == "" {
		return false, nil
	}
	owned := false
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		ownership, err := q.runLeaseOwnershipTx(tx, workerID, runID)
		if err != nil {
			return err
		}
		switch ownership {
		case runLeaseOwnedByWorker:
			owned = true

			return nil
		case runLeaseOwnedByAnotherWorker:
			return nil
		}
		matched, targetOwned, err := controlTargetOwnedBy(
			tx,
			q.leaseControlTargets,
			workerID,
			runID,
		)
		if err != nil || matched {
			owned = targetOwned

			return err
		}
		_, owned, err = controlTargetOwnedBy(
			tx,
			q.completedControlTargets,
			workerID,
			runID,
		)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("verify terminal crawl run owner: %w", err)
	}

	return owned, nil
}

func controlTargetOwnedBy(
	tx *vault.Txn,
	targets *vault.Collection[leaseControlTarget],
	workerID string,
	runID string,
) (bool, bool, error) {
	matched := false
	owned := false
	err := targets.Scan(tx, nil, func(_ vault.Key, target leaseControlTarget) (bool, error) {
		if target.RunID != runID {
			return true, nil
		}
		if matched && owned != (target.WorkerID == workerID) {
			owned = false

			return false, nil
		}
		matched = true
		owned = target.WorkerID == workerID

		return true, nil
	})
	if err != nil {
		return false, false, fmt.Errorf("scan crawl lease control targets: %w", err)
	}

	return matched, matched && owned, nil
}
