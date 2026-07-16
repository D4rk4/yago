package crawlorder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

const (
	msgOrderReceived         = "crawl order received"
	msgRunSeeded             = "crawl run seeded"
	msgProfileRegisterFailed = "crawl profile registration failed"
	msgOrderExpansionFailed  = "crawl order expansion failed"
	msgOrderAckFailed        = "crawl order ack failed"
	msgOrderNakFailed        = "crawl order nak failed"
	msgOrderTermFailed       = "crawl order term failed"
	msgOrderJoinedActiveRun  = "crawl order joined active run"
	msgCompletedOrderReplay  = "completed crawl order replay received"
)

type RequestExpander interface {
	Expand(
		ctx context.Context,
		requests []yagocrawlcontract.CrawlRequest,
	) ([]yagocrawlcontract.CrawlRequest, error)
}

type CrawlOrderConsumer struct {
	orders   boundedqueue.Receiver[CrawlOrderDelivery]
	frontier *frontier.Frontier
	expander RequestExpander
	progress ProgressReporter
	tally    RunTallySource
	active   *activeOrders
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
	switch c.active.claim(order.Provenance, delivery) {
	case activeOrderJoinsRun:
		slog.DebugContext(ctx, msgOrderJoinedActiveRun,
			slog.String("handle", order.Profile.Handle),
		)

		return
	case activeOrderAlreadyCompleted:
		slog.DebugContext(ctx, msgCompletedOrderReplay,
			slog.String("handle", order.Profile.Handle),
		)
		if err := delivery.Ack(context.WithoutCancel(ctx)); err != nil {
			slog.WarnContext(
				ctx,
				msgOrderAckFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}

		return
	}
	profile, requests, prepared := c.prepareCrawlOrder(ctx, order, delivery)
	if !prepared {
		return
	}
	c.reportRun(ctx, order, yagocrawlcontract.CrawlRunRunning, len(requests))
	reporter := newRunProgressReporter()
	c.frontier.Hold()
	seeded := c.frontier.SeedRunWithPriority(
		ctx,
		frontier.CrawlRunSeed{
			Requests:   requests,
			Provenance: order.Provenance,
			Priority:   order.Priority,
		},
		profile,
		c.finishRun(ctx, order, delivery, reporter),
	)
	reporter.reportWith(func() {
		c.reportRun(
			ctx,
			order,
			yagocrawlcontract.CrawlRunRunning,
			c.frontier.RunPending(seeded.RunID),
		)
	})
	reporter.start(ctx, progressReportInterval, order.Provenance)
	slog.InfoContext(
		ctx,
		msgRunSeeded,
		slog.String("handle", order.Profile.Handle),
		slog.String("runId", seeded.RunID.String()),
		slog.Int("queued", seeded.Queued),
	)
}

func (c *CrawlOrderConsumer) finishRun(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
	reporter *runProgressReporter,
) func(succeeded bool) {
	return func(succeeded bool) {
		reporter.Stop()
		defer c.frontier.Release()
		cancelled := ctx.Err() != nil || c.frontier.WasCancelled(order.Provenance)
		c.frontier.ClearCancelled(order.Provenance)
		state := yagocrawlcontract.CrawlRunFinished
		if cancelled {
			state = yagocrawlcontract.CrawlRunCancelled
		}
		c.reportRun(context.WithoutCancel(ctx), order, state, 0)
		if c.tally != nil {
			c.tally.Forget(order.Provenance)
		}
		retainCompletion := !cancelled && succeeded
		c.active.settle(
			order.Provenance,
			delivery,
			retainCompletion,
			func(delivery CrawlOrderDelivery) {
				settlementCtx := context.WithoutCancel(ctx)
				if cancelled || !succeeded {
					if err := delivery.Nak(settlementCtx); err != nil {
						slog.WarnContext(
							settlementCtx,
							msgOrderNakFailed,
							slog.String("handle", order.Profile.Handle),
							slog.Any("error", err),
						)
					}

					return
				}
				if err := delivery.Ack(settlementCtx); err != nil {
					slog.WarnContext(
						settlementCtx,
						msgOrderAckFailed,
						slog.String("handle", order.Profile.Handle),
						slog.Any("error", err),
					)
				}
			},
		)
	}
}

// reportRun emits a best-effort lifecycle snapshot keyed by the order provenance,
// which the node and worker share as the run identity.
func (c *CrawlOrderConsumer) reportRun(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	state yagocrawlcontract.CrawlRunState,
	pending int,
) {
	var tally yagocrawlcontract.CrawlRunTally
	if c.tally != nil {
		tally = c.tally.Snapshot(order.Provenance)
	}
	if pending > 0 {
		tally.Pending = uint64(pending)
	}
	if c.progress == nil {
		return
	}
	c.progress.ReportRun(ctx, RunReport{
		Provenance:     order.Provenance,
		ProfileHandle:  order.Profile.Handle,
		ProfileName:    order.Profile.Name,
		State:          state,
		Tally:          tally,
		PagesPerMinute: c.frontier.EffectivePagesPerMinute(order.Provenance),
	})
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
