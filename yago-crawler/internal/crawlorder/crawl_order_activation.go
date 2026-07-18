package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type preparedCrawlOrder struct {
	order    yagocrawlcontract.CrawlOrder
	delivery CrawlOrderDelivery
	identity []byte
	recovery frontier.RunRecovery
	profile  crawladmission.AdmissionProfile
	requests []yagocrawlcontract.CrawlRequest
}

func (c *CrawlOrderConsumer) resolveActiveClaim(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) bool {
	switch c.active.claim(order.Provenance, delivery, c.leaseRebinder(order.Provenance)) {
	case activeOrderRejected:
		slog.WarnContext(ctx, msgOrderDefinitionConflict,
			slog.String("handle", order.Profile.Handle),
		)
		settleCrawlOrder(
			context.WithoutCancel(ctx),
			order,
			delivery,
			crawlOrderTerminated,
		)

		return true
	case activeOrderJoinsRun:
		slog.DebugContext(ctx, msgOrderJoinedActiveRun,
			slog.String("handle", order.Profile.Handle),
		)

		return true
	case activeOrderAlreadyCompleted:
		slog.DebugContext(ctx, msgCompletedOrderReplay,
			slog.String("handle", order.Profile.Handle),
		)
		settlementCtx := context.WithoutCancel(ctx)
		if err := delivery.Ack(settlementCtx); err != nil {
			slog.WarnContext(
				settlementCtx,
				msgOrderAckFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		} else {
			c.forgetCheckpoint(settlementCtx, order)
		}

		return true
	}

	return false
}

func (c *CrawlOrderConsumer) seedPreparedCrawlOrder(
	ctx context.Context,
	prepared preparedCrawlOrder,
) {
	c.restoreRecoveredTally(prepared.order.Provenance, prepared.recovery)
	reporter := newRunProgressReporter()
	initialReported := make(chan struct{})
	finish := c.finishRun(ctx, prepared.order, prepared.delivery, reporter)
	c.frontier.Hold()
	seeded := c.frontier.SeedRunWithPriority(
		ctx,
		frontier.CrawlRunSeed{
			Requests:      prepared.requests,
			Provenance:    prepared.order.Provenance,
			Priority:      prepared.order.Priority,
			OrderIdentity: prepared.identity,
			LeaseID:       prepared.delivery.LeaseID,
		},
		prepared.profile,
		func(succeeded bool) {
			<-initialReported
			finish(succeeded)
		},
	)
	report := func(pending int) {
		if prepared.recovery.Checkpointed {
			c.reportRecoveredRun(
				ctx,
				recoveredRunReport{
					order:    prepared.order,
					state:    yagocrawlcontract.CrawlRunRunning,
					pending:  pending,
					recovery: prepared.recovery,
					leaseID:  prepared.delivery.LeaseID,
				},
			)
		} else {
			c.reportRun(
				ctx,
				prepared.order,
				yagocrawlcontract.CrawlRunRunning,
				pending,
				prepared.delivery.LeaseID,
			)
		}
	}
	reporter.reportWith(func() { report(c.frontier.RunPending(seeded.RunID)) })
	report(seeded.Queued)
	close(initialReported)
	reporter.start(ctx, progressReportInterval, prepared.order.Provenance)
	slog.InfoContext(
		ctx,
		msgRunSeeded,
		slog.String("handle", prepared.order.Profile.Handle),
		slog.String("runId", seeded.RunID.String()),
		slog.Int("queued", seeded.Queued),
	)
}
