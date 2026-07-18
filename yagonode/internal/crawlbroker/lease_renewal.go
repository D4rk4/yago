package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) renewLeases(
	ctx context.Context,
	workerID string,
	workerSessionID string,
	activeLeaseIDs []string,
) ([]string, time.Duration, error) {
	q.leaseMutation.RLock()
	candidates, refresh, err := q.leaseRenewalCandidates(
		ctx,
		workerID,
		workerSessionID,
		activeLeaseIDs,
		nowFunc(),
	)
	if err != nil {
		q.leaseMutation.RUnlock()

		return nil, 0, fmt.Errorf("heartbeat crawl leases: %w", err)
	}
	if !refresh {
		renewed, remaining := renewalResponse(candidates, nowFunc(), q.leaseTTL)
		q.leaseMutation.RUnlock()

		return renewed, remaining, nil
	}
	q.leaseMutation.RUnlock()
	beforeLeaseRenewalWrite()
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	committed, err := q.commitLeaseRenewals(ctx, workerID, workerSessionID, candidates, nowFunc())
	if err != nil {
		return nil, 0, fmt.Errorf("heartbeat crawl leases: %w", err)
	}

	renewed, remaining := renewalResponse(committed, nowFunc(), q.leaseTTL)

	return renewed, remaining, nil
}

func (q *DurableOrderQueue) leaseRenewalCandidates(
	ctx context.Context,
	workerID string,
	workerSessionID string,
	activeLeaseIDs []string,
	readAt time.Time,
) ([]leaseRenewalCandidate, bool, error) {
	seen := make(map[string]struct{}, len(activeLeaseIDs))
	candidates := make([]leaseRenewalCandidate, 0, len(activeLeaseIDs))
	refresh := false
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		for _, leaseID := range activeLeaseIDs {
			if leaseID == "" {
				continue
			}
			if _, duplicate := seen[leaseID]; duplicate {
				continue
			}
			seen[leaseID] = struct{}{}
			record, found, err := q.leases.Get(tx, vault.Key(leaseID))
			if err != nil {
				return fmt.Errorf("read crawl lease: %w", err)
			}
			if !found || !liveLeaseOwnedBy(record, workerID, workerSessionID, readAt) {
				continue
			}
			if time.Duration(record.ExpiresAtUnixNano-readAt.UnixNano()) <= q.leaseTTL*3/4 {
				refresh = true
			}
			candidates = append(candidates, leaseRenewalCandidate{leaseID: leaseID, record: record})
		}

		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("read renewable crawl leases: %w", err)
	}

	return candidates, refresh, nil
}

func (q *DurableOrderQueue) commitLeaseRenewals(
	ctx context.Context,
	workerID string,
	workerSessionID string,
	candidates []leaseRenewalCandidate,
	writeAt time.Time,
) ([]leaseRenewalCandidate, error) {
	deadline := writeAt.Add(q.leaseTTL)
	committed := make([]leaseRenewalCandidate, 0, len(candidates))
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		committed = committed[:0]
		for _, candidate := range candidates {
			record, found, err := q.leases.Get(tx, vault.Key(candidate.leaseID))
			if err != nil {
				return fmt.Errorf("read crawl lease: %w", err)
			}
			if !found || !liveLeaseOwnedBy(record, workerID, workerSessionID, writeAt) {
				continue
			}
			record.ExpiresAtUnixNano = deadline.UnixNano()
			if err := q.leases.Put(tx, vault.Key(candidate.leaseID), record); err != nil {
				return fmt.Errorf("extend crawl lease: %w", err)
			}
			committed = append(committed, leaseRenewalCandidate{
				leaseID: candidate.leaseID,
				record:  record,
			})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("commit crawl lease renewals: %w", err)
	}

	return committed, nil
}

func renewalResponse(
	candidates []leaseRenewalCandidate,
	responseAt time.Time,
	leaseTTL time.Duration,
) ([]string, time.Duration) {
	renewed := make([]string, 0, len(candidates))
	minimumRemaining := leaseTTL
	for _, candidate := range candidates {
		if candidate.record.ExpiresAtUnixNano <= responseAt.UnixNano() {
			continue
		}
		renewed = append(renewed, candidate.leaseID)
		remaining := time.Duration(candidate.record.ExpiresAtUnixNano - responseAt.UnixNano())
		minimumRemaining = min(minimumRemaining, remaining)
	}

	return renewed, minimumRemaining
}
