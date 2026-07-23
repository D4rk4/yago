package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const automaticDiscoverySettlementRecoveryBudget = time.Second

var afterAutomaticDiscoverySettlementStage = func() {}

func automaticDiscoverySettlementRecoveryContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(ctx),
		automaticDiscoverySettlementRecoveryBudget,
	)
}

func (q *DurableOrderQueue) resolveAutomaticDiscoverySettlement(
	ctx context.Context,
	leaseID string,
) error {
	resolution, err := q.completeAutomaticDiscoverySettlement(ctx, leaseID)
	if resolution.Acknowledged {
		q.workerLeases.remove(resolution.Intent.Lease)
		q.signal()
	}
	if err != nil {
		return err
	}

	return nil
}

func (q *DurableOrderQueue) automaticDiscoverySettlementLeaseIDsForWorker(
	ctx context.Context,
	workerID string,
) ([]string, error) {
	leaseIDs := make([]string, 0)
	if err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.discoverySettlements.Scan(tx, nil, func(
			key vault.Key,
			intent automaticDiscoverySettlementIntent,
		) (bool, error) {
			if intent.Lease.WorkerID == workerID {
				leaseIDs = append(leaseIDs, string(key))
			}

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf(
			"read worker automatic crawl discovery settlements: %w",
			err,
		)
	}

	return leaseIDs, nil
}

func (q *DurableOrderQueue) resolveWorkerAutomaticDiscoverySettlements(
	ctx context.Context,
	workerID string,
) error {
	leaseIDs, err := q.automaticDiscoverySettlementLeaseIDsForWorker(ctx, workerID)
	if err != nil {
		return err
	}
	if len(leaseIDs) == 0 {
		return nil
	}
	recoveryContext, cancel := automaticDiscoverySettlementRecoveryContext(ctx)
	defer cancel()
	for _, leaseID := range leaseIDs {
		if err := q.resolveAutomaticDiscoverySettlement(
			recoveryContext,
			leaseID,
		); err != nil {
			return err
		}
	}

	return nil
}
