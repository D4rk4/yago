package crawlbroker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) prepareTerminalLeaseSettlementTx(
	tx *vault.Txn,
	leaseID string,
	request terminalLeaseRequest,
	want leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	record, found, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("read crawl lease: %w", err)
	}
	if !found || record.Deferred {
		return q.requireTerminalLeaseSettlement(tx, leaseID, want)
	}
	if !liveLeaseOwnedBy(record, request.WorkerID, request.WorkerSessionID, nowFunc()) {
		return leaseSettlementRecord{}, errLeaseLost
	}
	identity := sha256.Sum256(record.OrderData)
	if !bytes.Equal(identity[:], request.OrderIdentity) {
		return leaseSettlementRecord{}, errLeaseDispositionConflict
	}
	want, err = terminalSettlementWithOrder(want, record.OrderData)
	if err != nil {
		return leaseSettlementRecord{}, err
	}
	if err := q.applyTerminalLeaseDispositionTx(tx, leaseID, request, want, record); err != nil {
		return leaseSettlementRecord{}, err
	}

	return q.recordTerminalLeaseSettlement(tx, leaseID, want)
}

func terminalSettlementWithOrder(
	settlement leaseSettlementRecord,
	orderData []byte,
) (leaseSettlementRecord, error) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(orderData)
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf("decode crawl lease order: %w", err)
	}
	settlement.Progress.RunID = hex.EncodeToString(order.Provenance)
	settlement.Progress.ProfileHandle = order.Profile.Handle
	settlement.Progress.ProfileName = order.Profile.Name
	if err := validateTerminalLeaseDefinition(
		settlement.OrderIdentity,
		settlement.Progress,
	); err != nil {
		return leaseSettlementRecord{}, err
	}

	return settlement, nil
}

func (q *DurableOrderQueue) applyTerminalLeaseDispositionTx(
	tx *vault.Txn,
	leaseID string,
	request terminalLeaseRequest,
	settlement leaseSettlementRecord,
	record leaseRecord,
) error {
	if request.Outcome == leaseSettlementAcknowledged {
		target := leaseControlTarget{WorkerID: request.WorkerID, RunID: settlement.Progress.RunID}
		if err := q.leaseControlTargets.Put(tx, vault.Key(leaseID), target); err != nil {
			return fmt.Errorf("store crawl lease control target: %w", err)
		}
		if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
			return fmt.Errorf("delete crawl lease: %w", err)
		}

		return nil
	}
	record.WorkerID = ""
	record.Deferred = true
	record.ExpiresAtUnixNano = nowFunc().Add(negativeAcknowledgmentRetryDelay).UnixNano()
	if err := q.leases.Put(tx, vault.Key(leaseID), record); err != nil {
		return fmt.Errorf("defer crawl lease: %w", err)
	}

	return nil
}
