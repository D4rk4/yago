package crawlorder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
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
		progress: noopProgressReporter{},
		tally:    noopRunTallySource{},
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

func (c *CrawlOrderConsumer) accept(ctx context.Context, delivery CrawlOrderDelivery) {
	order := delivery.Order
	slog.InfoContext(
		ctx,
		msgOrderReceived,
		slog.String("handle", order.Profile.Handle),
		slog.Int("seeds", len(order.Requests)),
	)
	profile, err := crawladmission.CompileProfile(order.Profile)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgProfileRegisterFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		if err := delivery.Term(ctx); err != nil {
			slog.WarnContext(
				ctx,
				msgOrderTermFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}
		return
	}
	requests, err := c.expander.Expand(ctx, order.Requests)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgOrderExpansionFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		if err := delivery.Term(ctx); err != nil {
			slog.WarnContext(
				ctx,
				msgOrderTermFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}
		return
	}
	c.reportRun(ctx, order, yagocrawlcontract.CrawlRunRunning, len(requests))
	reporter := newRunProgressReporter()
	c.frontier.Hold()
	seeded := c.frontier.SeedRun(
		ctx,
		requests,
		order.Provenance,
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
	reporter.start(ctx, progressReportInterval)
	slog.InfoContext(
		ctx,
		msgRunSeeded,
		slog.String("handle", order.Profile.Handle),
		slog.String("runId", seeded.RunID.String()),
		slog.Int("queued", seeded.Queued),
	)
}

// finishRun builds the run's completion callback: it reports the terminal run
// state (cancelled during shutdown, finished otherwise) and settles the order's
// lease. A run that drained with succeeded=false lost at least one page's
// references in delivery, so it naks down the same redelivery path as a
// cancelled run rather than acking with those references lost. The report uses a
// cancel-detached context so a report still reaches the node while the worker is
// draining on shutdown.
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
		c.tally.Forget(order.Provenance)
		if cancelled || !succeeded {
			if err := delivery.Nak(context.Background()); err != nil {
				slog.WarnContext(
					context.Background(),
					msgOrderNakFailed,
					slog.String("handle", order.Profile.Handle),
					slog.Any("error", err),
				)
			}

			return
		}
		if err := delivery.Ack(context.Background()); err != nil {
			slog.WarnContext(
				context.Background(),
				msgOrderAckFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}
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
	tally := c.tally.Snapshot(order.Provenance)
	if pending > 0 {
		tally.Pending = uint64(pending)
	}
	c.progress.ReportRun(ctx, RunReport{
		Provenance:    order.Provenance,
		ProfileHandle: order.Profile.Handle,
		ProfileName:   order.Profile.Name,
		State:         state,
		Tally:         tally,
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
