package crawlbroker

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errLeaseLost = errors.New("crawl lease lost")

var beforeLeaseRenewalWrite = func() {}

type leaseAuthorization struct {
	LeaseID         string
	WorkerID        string
	WorkerSessionID string
	RunID           string
}

type leaseRenewalCandidate struct {
	leaseID string
	record  leaseRecord
}

func liveLeaseOwnedBy(
	record leaseRecord,
	workerID string,
	workerSessionID string,
	at time.Time,
) bool {
	return workerID != "" && workerSessionID != "" && !record.Deferred &&
		record.WorkerID == workerID && record.WorkerSessionID == workerSessionID &&
		record.ExpiresAtUnixNano > at.UnixNano()
}

func (q *DurableOrderQueue) beginAuthorizedLeaseMutation(
	ctx context.Context,
	authorization leaseAuthorization,
) (func(), error) {
	group := q.authorizedLeaseMutationGroup(ctx)
	if !group {
		q.leaseMutation.RLock()
	}
	if err := q.verifyLeaseAuthorization(ctx, authorization); err != nil {
		if !group {
			q.leaseMutation.RUnlock()
		}

		return nil, err
	}

	if group {
		return func() {}, nil
	}

	return q.leaseMutation.RUnlock, nil
}

func (q *DurableOrderQueue) verifyLeaseAuthorization(
	ctx context.Context,
	authorization leaseAuthorization,
) error {
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		record, found, err := q.leases.Get(tx, vault.Key(authorization.LeaseID))
		if err != nil {
			return fmt.Errorf("read crawl lease: %w", err)
		}
		if !found || !liveLeaseOwnedBy(
			record,
			authorization.WorkerID,
			authorization.WorkerSessionID,
			nowFunc(),
		) {
			return errLeaseLost
		}
		if authorization.RunID == "" {
			return nil
		}
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(record.OrderData)
		if err != nil {
			return fmt.Errorf("decode crawl lease order: %w", err)
		}
		if hex.EncodeToString(order.Provenance) != authorization.RunID {
			return errLeaseLost
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("verify crawl lease authorization: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) adoptWorkerSession(
	ctx context.Context,
	workerID string,
	workerSessionID string,
) ([]leasedCrawlOrder, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	now := nowFunc()
	deadline := now.Add(q.leaseTTL).UnixNano()
	leases := make([]leasedCrawlOrder, 0)
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		leases = leases[:0]
		keys := make([]vault.Key, 0)
		records := make([]leaseRecord, 0)
		if err := q.leases.Scan(tx, nil, func(
			key vault.Key,
			record leaseRecord,
		) (bool, error) {
			if record.WorkerID != workerID || record.Deferred {
				return true, nil
			}
			record.WorkerSessionID = workerSessionID
			record.ExpiresAtUnixNano = deadline
			keys = append(keys, key)
			records = append(records, record)
			leases = append(leases, leasedCrawlOrder{
				LeaseID:   string(key),
				OrderData: append([]byte(nil), record.OrderData...),
			})

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan worker crawl leases: %w", err)
		}
		for index, key := range keys {
			if err := q.leases.Put(tx, key, records[index]); err != nil {
				return fmt.Errorf("assign worker crawl session: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("adopt worker crawl leases: %w", err)
	}

	return leases, nil
}
