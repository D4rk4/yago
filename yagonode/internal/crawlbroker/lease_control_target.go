package crawlbroker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	leaseControlTargetBucket          vault.Name = "crawlordercontroltargets"
	completedLeaseControlTargetBucket vault.Name = "completedcrawlordercontroltargets"
)

type leaseControlTarget struct {
	WorkerID string `json:"worker"`
	RunID    string `json:"run"`
}

type leaseControlTargetCodec struct{}

type runLeaseOwnership uint8

const (
	runLeaseUnclaimed runLeaseOwnership = iota
	runLeaseOwnedByWorker
	runLeaseOwnedByAnotherWorker
)

func (leaseControlTargetCodec) Encode(target leaseControlTarget) ([]byte, error) {
	raw, _ := json.Marshal(target)

	return raw, nil
}

func (leaseControlTargetCodec) Decode(raw []byte) (leaseControlTarget, error) {
	var target leaseControlTarget
	if err := json.Unmarshal(raw, &target); err != nil {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease control target: %w", err)
	}
	if target.WorkerID == "" || target.RunID == "" {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease control target: empty identity")
	}

	return target, nil
}

func controlTargetFromLease(record leaseRecord) (leaseControlTarget, error) {
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(record.OrderData)
	if err != nil {
		return leaseControlTarget{}, fmt.Errorf("decode crawl lease order: %w", err)
	}

	return leaseControlTarget{
		WorkerID: record.WorkerID,
		RunID:    hex.EncodeToString(order.Provenance),
	}, nil
}

func (q *DurableOrderQueue) runLeaseOwnership(
	ctx context.Context,
	workerID string,
	runID string,
) (runLeaseOwnership, error) {
	if workerID == "" || runID == "" {
		return runLeaseUnclaimed, nil
	}
	ownership := runLeaseUnclaimed
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		ownership, err = q.runLeaseOwnershipTx(tx, workerID, runID)

		return err
	})
	if err != nil {
		return runLeaseUnclaimed, fmt.Errorf("verify crawl run lease owner: %w", err)
	}

	return ownership, nil
}

func (q *DurableOrderQueue) runLeaseOwnershipTx(
	tx *vault.Txn,
	workerID string,
	runID string,
) (runLeaseOwnership, error) {
	ownership := runLeaseUnclaimed
	err := q.leases.Scan(tx, nil, func(_ vault.Key, record leaseRecord) (bool, error) {
		if record.Deferred {
			return true, nil
		}
		target, err := controlTargetFromLease(record)
		if err != nil {
			return false, err
		}
		if target.RunID != runID {
			return true, nil
		}
		if record.WorkerID == workerID {
			ownership = runLeaseOwnedByWorker

			return false, nil
		}
		ownership = runLeaseOwnedByAnotherWorker
		return true, nil
	})
	if err != nil {
		return runLeaseUnclaimed, fmt.Errorf("scan crawl run leases: %w", err)
	}

	return ownership, nil
}
