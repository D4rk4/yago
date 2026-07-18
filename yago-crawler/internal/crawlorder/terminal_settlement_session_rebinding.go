package crawlorder

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type terminalSettlementSession struct {
	workerID        string
	workerSessionID string
	leaseGrants     *crawllease.GrantRegistry
}

func (relay *terminalSettlementRelay) bindWorkerLeaseSession(
	workerID string,
	workerSessionID string,
	leaseGrants *crawllease.GrantRegistry,
) {
	relay.session = terminalSettlementSession{
		workerID:        workerID,
		workerSessionID: workerSessionID,
		leaseGrants:     leaseGrants,
	}
}

func (relay *terminalSettlementRelay) rebindAwaitingWorkerSession(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) (crawlsettlement.Settlement, bool, error) {
	session := relay.session
	if settlement.Phase != crawlsettlement.AwaitingAcknowledgment ||
		settlement.WorkerSessionID == session.workerSessionID ||
		settlement.WorkerID != session.workerID || session.leaseGrants == nil ||
		!yagocrawlcontract.ValidCrawlerSessionIdentity(session.workerSessionID) ||
		!session.leaseGrants.Confirmed(settlement.LeaseID) {
		return settlement, true, nil
	}
	rebound, unchanged, err := relay.outbox.RebindWorkerSession(
		ctx,
		settlement,
		session.workerSessionID,
	)
	if err != nil {
		return crawlsettlement.Settlement{}, false, fmt.Errorf(
			"rebind terminal settlement worker session: %w",
			err,
		)
	}

	return rebound, unchanged, nil
}
