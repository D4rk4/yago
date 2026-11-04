package crawlorder

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

func leaseSessionCrawlOrderDelivery(
	ctx context.Context,
	envelope crawlOrderDeliveryEnvelope,
) CrawlOrderDelivery {
	workerSessionID := ""
	var leaseGrants *crawllease.GrantRegistry
	if envelope.heartbeat != nil {
		workerSessionID = envelope.heartbeat.workerSessionID
		leaseGrants = envelope.heartbeat.leaseGrants
	}
	acknowledgment := leasedOrderAcknowledgment{
		leaseID:         envelope.leaseID,
		workerID:        envelope.workerID,
		workerSessionID: workerSessionID,
	}
	requeue := acknowledgment
	requeue.requeue = true
	delivery := CrawlOrderDelivery{
		LeaseID:       envelope.leaseID,
		Order:         envelope.order,
		OrderIdentity: envelope.orderIdentity,
		Ack: leaseSessionSettlement(
			ctx,
			envelope,
			acknowledgment,
			leaseGrants,
		),
		Nak: leaseSessionSettlement(ctx, envelope, requeue, leaseGrants),
		NakAfter: func(settlementCtx context.Context, retryAfter time.Duration) error {
			deferred := requeue
			deferred.retryAfter = retryAfter
			return leaseSessionSettlement(ctx, envelope, deferred, leaseGrants)(settlementCtx)
		},
		Term: leaseSessionSettlement(
			ctx,
			envelope,
			acknowledgment,
			leaseGrants,
		),
	}
	if envelope.terminalSettlements != nil {
		delivery.settleTerminal = terminalCrawlOrderSettlement(envelope, workerSessionID)
	}
	return delivery
}

func leaseSessionSettlement(
	ctx context.Context,
	envelope crawlOrderDeliveryEnvelope,
	acknowledgment leasedOrderAcknowledgment,
	leaseGrants *crawllease.GrantRegistry,
) func(context.Context) error {
	return settleGrantedLease(
		settleLeaseForSession(ctx, envelope.client, acknowledgment),
		envelope.leaseID,
		leaseGrants,
	)
}

func terminalCrawlOrderSettlement(
	envelope crawlOrderDeliveryEnvelope,
	workerSessionID string,
) func(context.Context, terminalRunSettlement) error {
	return func(
		settlementCtx context.Context,
		terminal terminalRunSettlement,
	) error {
		outcome := crawlsettlement.Delete
		if terminal.Disposition == crawlOrderRequeued {
			outcome = crawlsettlement.Requeue
		}
		return envelope.terminalSettlements.stageAndDeliver(
			settlementCtx,
			crawlsettlement.Settlement{
				LeaseID:         envelope.leaseID,
				OrderIdentity:   append([]byte(nil), envelope.orderIdentity...),
				Provenance:      append([]byte(nil), envelope.order.Provenance...),
				WorkerID:        envelope.workerID,
				WorkerSessionID: workerSessionID,
				Outcome:         outcome,
				State:           terminal.State,
				Tally:           terminal.Tally,
				RecentOutcomes:  terminal.RecentOutcomes,
				PagesPerMinute:  terminal.PagesPerMinute,
				RateKnown:       terminal.RateKnown,
			},
		)
	}
}
