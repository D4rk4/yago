package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type negativeAcknowledgmentMutation struct {
	lease   leaseRecord
	staged  bool
	removed bool
}

func (q *DurableOrderQueue) negativeAcknowledgmentTxAfter(
	tx *vault.Txn,
	deferral leaseDeferral,
) (negativeAcknowledgmentMutation, error) {
	_, staged, err := q.discoverySettlements.Get(tx, vault.Key(deferral.leaseID))
	if err != nil {
		return negativeAcknowledgmentMutation{}, fmt.Errorf(
			"read automatic crawl discovery settlement: %w",
			err,
		)
	}
	if staged {
		return negativeAcknowledgmentMutation{staged: true}, nil
	}
	record, found, err := q.leases.Get(tx, vault.Key(deferral.leaseID))
	if err != nil {
		return negativeAcknowledgmentMutation{}, fmt.Errorf("read crawl lease: %w", err)
	}
	record, mutable, err := negativeAcknowledgmentLease(
		record,
		found,
		deferral,
	)
	if err != nil || !mutable {
		return negativeAcknowledgmentMutation{}, err
	}
	if err := q.recordLeaseSettlement(tx, deferral.leaseID, leaseSettlementRequeued); err != nil {
		return negativeAcknowledgmentMutation{}, err
	}
	removed := record
	record.WorkerID = ""
	record.Deferred = true
	record.ExpiresAtUnixNano = nowFunc().Add(deferral.retryAfter).UnixNano()
	if err := q.leases.Put(tx, vault.Key(deferral.leaseID), record); err != nil {
		return negativeAcknowledgmentMutation{}, fmt.Errorf("defer crawl lease: %w", err)
	}

	return negativeAcknowledgmentMutation{lease: removed, removed: true}, nil
}

func negativeAcknowledgmentLease(
	record leaseRecord,
	found bool,
	deferral leaseDeferral,
) (leaseRecord, bool, error) {
	if !found {
		return leaseRecord{}, false, errLeaseDispositionConflict
	}
	if record.WorkerID == "" {
		if record.Deferred {
			return record, false, nil
		}

		return leaseRecord{}, false, errLeaseDispositionConflict
	}
	if deferral.requireOwner && !liveLeaseOwnedBy(
		record,
		deferral.workerID,
		deferral.workerSessionID,
		nowFunc(),
	) {
		return leaseRecord{}, false, errLeaseLost
	}

	return record, true, nil
}
