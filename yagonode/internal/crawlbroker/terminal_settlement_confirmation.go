package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) confirmTerminalLeaseSettlement(
	ctx context.Context,
	leaseID string,
	request terminalLeaseRequest,
) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	requeued := false
	err := q.vault.Update(ctx, func(transaction *vault.Txn) error {
		var err error
		requeued, err = q.confirmTerminalLeaseSettlementTx(transaction, leaseID, request)

		return err
	})
	if err != nil {
		return fmt.Errorf("confirm terminal crawl lease: %w", err)
	}
	if requeued {
		q.signal()
	}

	return nil
}

func terminalSettlementRecord(request terminalLeaseRequest) leaseSettlementRecord {
	return leaseSettlementRecord{
		Outcome:         request.Outcome,
		OrderIdentity:   append([]byte(nil), request.OrderIdentity...),
		WorkerSessionID: request.WorkerSessionID,
		Progress: yagocrawlcontract.CrawlRunProgress{
			WorkerID:       request.WorkerID,
			State:          request.State,
			Tally:          request.Tally,
			RecentOutcomes: request.RecentOutcomes,
			PagesPerMinute: request.Rate,
			RateKnown:      request.RateKnown,
		},
		Terminal: true,
	}
}
