package crawlorder

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const checkpointInspectionTimeout = 5 * time.Second

type recoveredRunReport struct {
	order    yagocrawlcontract.CrawlOrder
	state    yagocrawlcontract.CrawlRunState
	pending  int
	recovery frontier.RunRecovery
	leaseID  string
}

func (c *CrawlOrderConsumer) checkpointRecovery(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) ([]byte, frontier.RunRecovery, bool) {
	identity, err := crawlOrderDeliveryIdentity(delivery)
	if err == nil {
		inspectionCtx, cancelInspection := context.WithTimeout(
			context.WithoutCancel(ctx),
			checkpointInspectionTimeout,
		)
		var recovery frontier.RunRecovery
		recovery, err = c.frontier.Recovery(inspectionCtx, order.Provenance, identity)
		cancelInspection()
		if err == nil {
			return identity, recovery, true
		}
	}
	c.frontier.RecordCheckpointFailure(err)
	slog.WarnContext(ctx,
		msgOrderCheckpointFailed,
		slog.String("handle", order.Profile.Handle),
		slog.Any("error", err),
	)
	c.requeueOrder(ctx, order, delivery)

	return nil, frontier.RunRecovery{}, false
}

func (c *CrawlOrderConsumer) prepareCrawlOrderForRecovery(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
	recovery frontier.RunRecovery,
) (crawladmission.AdmissionProfile, []yagocrawlcontract.CrawlRequest, bool) {
	if !recovery.Checkpointed {
		return c.prepareCrawlOrder(ctx, order, delivery)
	}
	if recovery.Seeding {
		if !recovery.SeedManifest {
			c.retainOrder(ctx, order, delivery)
			return crawladmission.AdmissionProfile{}, nil, false
		}
		return c.prepareRecoveredSeedingOrder(ctx, order, delivery)
	}
	profile, prepared := c.compileCrawlOrder(ctx, order, delivery)

	return profile, nil, prepared
}

func (c *CrawlOrderConsumer) settleRecoveredOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
	recovery frontier.RunRecovery,
) {
	c.restoreRecoveredTally(order.Provenance, recovery)
	state := yagocrawlcontract.CrawlRunFinished
	if recovery.Cancelled {
		state = yagocrawlcontract.CrawlRunCancelled
	}
	tally := c.recoveredRunTally(order.Provenance, recovery, 0)
	recentOutcomes := c.recentRunOutcomes(order.Provenance)
	if delivery.settleTerminal == nil {
		c.reportRunTally(
			context.WithoutCancel(ctx),
			order,
			state,
			tally,
			delivery.LeaseID,
		)
	}
	disposition := crawlOrderAcknowledged
	retainCompletion := true
	if recovery.Cancelled {
		disposition = crawlOrderTerminated
	}
	settleActive := c.active.settle
	if delivery.settleTerminal != nil {
		settleActive = c.active.settleDurably
	}
	settled := settleActive(
		order.Provenance,
		delivery,
		retainCompletion,
		func(delivery CrawlOrderDelivery) bool {
			return settleTerminalCrawlOrder(
				context.WithoutCancel(ctx),
				terminalCrawlOrderDisposition{
					order:          order,
					delivery:       delivery,
					disposition:    disposition,
					state:          state,
					tally:          tally,
					recentOutcomes: recentOutcomes,
					pagesPerMinute: c.frontier.EffectivePagesPerMinute(order.Provenance),
				},
			)
		},
	)
	if c.tally != nil {
		c.tally.Forget(order.Provenance)
	}
	if settled && delivery.settleTerminal == nil && c.frontier.CheckpointFailure() == nil {
		c.forgetCheckpoint(context.WithoutCancel(ctx), order)
	}
}

func (c *CrawlOrderConsumer) restoreRecoveredTally(
	provenance []byte,
	recovery frontier.RunRecovery,
) {
	if c.tally == nil || !recovery.Checkpointed {
		return
	}
	c.tally.Restore(provenance, normalizedRecoveredTally(recovery))
}

func (c *CrawlOrderConsumer) reportRecoveredRun(
	ctx context.Context,
	report recoveredRunReport,
) {
	tally := c.recoveredRunTally(report.order.Provenance, report.recovery, report.pending)
	c.reportRunTally(ctx, report.order, report.state, tally, report.leaseID)
}

func (c *CrawlOrderConsumer) recoveredRunTally(
	provenance []byte,
	recovery frontier.RunRecovery,
	pending int,
) yagocrawlcontract.CrawlRunTally {
	tally := normalizedRecoveredTally(recovery)
	if c.tally != nil {
		tally = c.tally.Snapshot(provenance)
	}
	tally.Pending = 0
	if pending > 0 {
		tally.Pending = uint64(pending)
	}

	return tally
}

func normalizedRecoveredTally(recovery frontier.RunRecovery) yagocrawlcontract.CrawlRunTally {
	tally := recovery.Tally
	tally.Pending = 0
	if recovery.Failed && tally.Failed == 0 {
		tally.Failed = 1
	}

	return tally
}

func (c *CrawlOrderConsumer) forgetCheckpoint(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
) {
	if err := c.frontier.ForgetCheckpoint(ctx, order.Provenance); err != nil {
		slog.WarnContext(ctx,
			msgOrderCheckpointFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
	}
}
