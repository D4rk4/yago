package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) acknowledgeLeaseTx(
	tx *vault.Txn,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) (leaseControlTarget, error) {
	record, found, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseControlTarget{}, fmt.Errorf("read crawl lease: %w", err)
	}
	if !found {
		return q.acknowledgedLeaseTargetTx(tx, leaseID)
	}
	if record.Deferred {
		return leaseControlTarget{}, errLeaseDispositionConflict
	}
	if requireOwner && !liveLeaseOwnedBy(record, workerID, workerSessionID, nowFunc()) {
		return leaseControlTarget{}, errLeaseLost
	}
	target, err := controlTargetFromLease(record)
	if err != nil {
		return leaseControlTarget{}, err
	}
	if err := q.persistAcknowledgedLeaseTx(tx, leaseID, target); err != nil {
		return leaseControlTarget{}, err
	}

	return target, nil
}

func (q *DurableOrderQueue) acknowledgedLeaseTargetTx(
	tx *vault.Txn,
	leaseID string,
) (leaseControlTarget, error) {
	target, _, err := q.leaseControlTargets.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseControlTarget{}, fmt.Errorf("read crawl lease control target: %w", err)
	}
	if err := q.requireLeaseSettlement(tx, leaseID, leaseSettlementAcknowledged); err != nil {
		return leaseControlTarget{}, err
	}

	return target, nil
}

func (q *DurableOrderQueue) persistAcknowledgedLeaseTx(
	tx *vault.Txn,
	leaseID string,
	target leaseControlTarget,
) error {
	if target.WorkerID != "" && target.RunID != "" {
		if err := q.leaseControlTargets.Put(tx, vault.Key(leaseID), target); err != nil {
			return fmt.Errorf("store crawl lease control target: %w", err)
		}
	}
	if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
		return fmt.Errorf("delete crawl lease: %w", err)
	}
	if err := q.recordLeaseSettlement(tx, leaseID, leaseSettlementAcknowledged); err != nil {
		return err
	}

	return nil
}
