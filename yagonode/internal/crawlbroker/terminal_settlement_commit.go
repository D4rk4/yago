package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) confirmTerminalLeaseSettlementTx(
	tx *vault.Txn,
	leaseID string,
	request terminalLeaseRequest,
) (bool, error) {
	settlement, found, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read terminal crawl settlement: %w", err)
	}
	if !found {
		return false, nil
	}
	if !sameTerminalLeaseSettlement(settlement, terminalSettlementRecord(request)) {
		return false, errLeaseDispositionConflict
	}
	requeued := false
	if request.Outcome == leaseSettlementRequeued {
		requeued, err = q.requeueTerminalLeaseTx(tx, leaseID, request.OrderIdentity)
		if err != nil {
			return false, err
		}
	}
	if err := q.deleteTerminalLeaseSettlementTx(tx, leaseID, settlement); err != nil {
		return false, err
	}

	return requeued, nil
}

func (q *DurableOrderQueue) deleteTerminalLeaseSettlementTx(
	tx *vault.Txn,
	leaseID string,
	settlement leaseSettlementRecord,
) error {
	if _, err := q.leaseSettlements.Delete(tx, vault.Key(leaseID)); err != nil {
		return fmt.Errorf("delete terminal crawl settlement: %w", err)
	}
	if _, err := q.leaseSettlementOrder.Delete(tx, orderKey(settlement.Sequence)); err != nil {
		return fmt.Errorf("delete terminal crawl settlement index: %w", err)
	}
	if expiryKey := leaseSettlementExpiryKey(settlement); expiryKey != nil {
		if _, err := q.leaseSettlementExpiry.Delete(tx, expiryKey); err != nil {
			return fmt.Errorf("delete terminal crawl settlement expiry: %w", err)
		}
	}
	if _, err := q.leaseControlTargets.Delete(tx, vault.Key(leaseID)); err != nil {
		return fmt.Errorf("delete crawl lease control target: %w", err)
	}
	if _, err := q.completedControlTargets.Delete(tx, vault.Key(leaseID)); err != nil {
		return fmt.Errorf("delete completed crawl lease control target: %w", err)
	}

	return nil
}
