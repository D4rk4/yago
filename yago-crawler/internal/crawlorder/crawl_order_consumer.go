package crawlorder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	msgOrderReceived           = "crawl order received"
	msgRunSeeded               = "crawl run seeded"
	msgProfileRegisterFailed   = "crawl profile registration failed"
	msgOrderExpansionFailed    = "crawl order expansion failed"
	msgOrderAckFailed          = "crawl order ack failed"
	msgOrderNakFailed          = "crawl order nak failed"
	msgOrderTermFailed         = "crawl order term failed"
	msgOrderJoinedActiveRun    = "crawl order joined active run"
	msgCompletedOrderReplay    = "completed crawl order replay received"
	msgOrderDefinitionConflict = "crawl order conflicts with active run"
	msgOrderCheckpointFailed   = "crawl order checkpoint failed"
)

type RequestExpander interface {
	Expand(
		ctx context.Context,
		requests []yagocrawlcontract.CrawlRequest,
	) ([]yagocrawlcontract.CrawlRequest, error)
}

type CrawlOrderConsumer struct {
	orders          boundedqueue.Receiver[CrawlOrderDelivery]
	frontier        *frontier.Frontier
	expander        RequestExpander
	progress        ProgressReporter
	tally           RunTallySource
	active          *activeOrders
	growthAdmission GrowthAdmission
	activeRuns      *ActiveRunAdmission
}

type GrowthAdmission interface {
	WaitForGrowth(context.Context) bool
}

func (c *CrawlOrderConsumer) WithGrowthAdmission(
	admission GrowthAdmission,
) *CrawlOrderConsumer {
	c.growthAdmission = admission

	return c
}

func (c *CrawlOrderConsumer) WithActiveRunAdmission(
	admission *ActiveRunAdmission,
) *CrawlOrderConsumer {
	c.activeRuns = admission

	return c
}

func NewCrawlOrderConsumer(
	orders boundedqueue.Receiver[CrawlOrderDelivery],
	frontier *frontier.Frontier,
	expander ...RequestExpander,
) *CrawlOrderConsumer {
	selected := RequestExpander(passThroughRequestExpander{})
	if len(expander) > 0 && expander[0] != nil {
		selected = expander[0]
	}
	return &CrawlOrderConsumer{
		orders:   orders,
		frontier: frontier,
		expander: selected,
		active:   newActiveOrders(),
	}
}

// WithProgressReporter attaches a reporter that receives run lifecycle snapshots
// as runs start and finish. A nil reporter is ignored so the default no-op stays.
func (c *CrawlOrderConsumer) WithProgressReporter(reporter ProgressReporter) *CrawlOrderConsumer {
	if reporter != nil {
		c.progress = reporter
	}

	return c
}

// WithRunTally attaches the per-run outcome tally read into each run's finish
// report. A nil tally is ignored so the default empty tally stays.
func (c *CrawlOrderConsumer) WithRunTally(tally RunTallySource) *CrawlOrderConsumer {
	if tally != nil {
		c.tally = tally
	}

	return c
}

func (c *CrawlOrderConsumer) Run(ctx context.Context) {
	c.frontier.Hold()
	defer c.frontier.Release()
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-c.orders.Receive():
			if !ok {
				return
			}
			c.accept(ctx, delivery)
		}
	}
}

func (c *CrawlOrderConsumer) CancelActiveRuns() {
	for _, provenance := range c.active.provenances() {
		c.frontier.Cancel(provenance)
	}
}

func (c *CrawlOrderConsumer) WaitForSettlements() {
	c.frontier.WaitForSettlements()
}

func (c *CrawlOrderConsumer) accept(ctx context.Context, delivery CrawlOrderDelivery) {
	order := delivery.Order
	slog.InfoContext(
		ctx,
		msgOrderReceived,
		slog.String("handle", order.Profile.Handle),
		slog.Int("seeds", len(order.Requests)),
	)
	if c.resolveActiveClaim(ctx, order, delivery) {
		return
	}
	identity, recovery, recovered := c.checkpointRecovery(ctx, order, delivery)
	if !recovered {
		return
	}
	if recovery.Completed || recovery.Cancelled {
		c.settleRecoveredOrder(ctx, order, delivery, recovery)

		return
	}
	if ctx.Err() != nil {
		c.retainOrder(ctx, order, delivery)

		return
	}
	if !recovery.Checkpointed && c.growthAdmission != nil &&
		!c.growthAdmission.WaitForGrowth(ctx) {
		c.retainOrder(ctx, order, delivery)

		return
	}
	profile, requests, prepared := c.prepareCrawlOrderForRecovery(
		ctx,
		order,
		delivery,
		recovery,
	)
	if !prepared {
		return
	}
	releaseActiveRun := func() {}
	if c.activeRuns != nil {
		var admitted bool
		releaseActiveRun, admitted = c.activeRuns.acquire(ctx)
		if !admitted {
			c.retainOrder(ctx, order, delivery)

			return
		}
	}
	c.seedPreparedCrawlOrder(ctx, preparedCrawlOrder{
		order:    order,
		delivery: delivery,
		identity: identity,
		recovery: recovery,
		profile:  profile,
		requests: requests,
	}, releaseActiveRun)
}

func (c *CrawlOrderConsumer) finishRun(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
	reporter *runProgressReporter,
) func(succeeded bool) {
	startedDuringShutdown := ctx.Err() != nil
	return func(succeeded bool) {
		reporter.Stop()
		defer c.frontier.Release()
		suspended := c.frontier.WasSuspended(order.Provenance)
		cancelled := c.frontier.WasCancelled(order.Provenance)
		c.frontier.ClearCancelled(order.Provenance)
		c.frontier.ClearSuspended(order.Provenance)
		state := yagocrawlcontract.CrawlRunFinished
		if cancelled || startedDuringShutdown {
			state = yagocrawlcontract.CrawlRunCancelled
		}
		terminalTally := c.runTally(order.Provenance, 0)
		if !suspended && !startedDuringShutdown && delivery.settleTerminal == nil {
			c.reportRunTally(
				context.WithoutCancel(ctx),
				order,
				state,
				terminalTally,
				delivery.LeaseID,
			)
		}
		if c.tally != nil {
			c.tally.Forget(order.Provenance)
		}
		disposition := crawlOrderAcknowledged
		retainCompletion := true
		forgetCheckpoint := true
		switch {
		case cancelled:
			disposition = crawlOrderTerminated
		case suspended || startedDuringShutdown:
			disposition = crawlOrderRetained
			retainCompletion = false
			forgetCheckpoint = false
		case !succeeded:
			disposition = crawlOrderRequeued
			retainCompletion = false
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
						tally:          terminalTally,
						pagesPerMinute: c.frontier.EffectivePagesPerMinute(order.Provenance),
					},
				)
			},
		)
		if settled && forgetCheckpoint && delivery.settleTerminal == nil &&
			c.frontier.CheckpointFailure() == nil {
			c.forgetCheckpoint(context.WithoutCancel(ctx), order)
		}
	}
}

type passThroughRequestExpander struct{}

func (passThroughRequestExpander) Expand(
	_ context.Context,
	requests []yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	out := make([]yagocrawlcontract.CrawlRequest, 0, len(requests))
	for _, request := range requests {
		mode, ok := yagocrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !ok {
			return nil, fmt.Errorf("unsupported crawl request mode %q", request.Mode)
		}
		request.Mode = mode
		out = append(out, request)
	}
	return out, nil
}
