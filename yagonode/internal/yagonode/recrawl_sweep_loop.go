package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

const (
	defaultRecrawlSweepInterval   = time.Minute
	defaultRecrawlSweepBatch      = 256
	msgRecrawlSweepClaimFailed    = "recrawl sweep claim failed"
	msgRecrawlSweepProfileMissing = "recrawl sweep skipped url with unknown profile"
	msgRecrawlSweepPublishFailed  = "recrawl sweep publish failed"
	msgRecrawlSwept               = "recrawl sweep re-dispatched due urls"
)

// recrawlPublisher enqueues a re-dispatched crawl order. The durable order queue
// satisfies it via Publish; recrawls use the keyless Publish so a due URL is never
// swallowed as a duplicate of its original crawl.
type recrawlPublisher interface {
	Publish(ctx context.Context, order yagocrawlcontract.CrawlOrder) error
}

// recrawlSweeper drains the recrawl schedule: each pass claims the URLs that have
// come due and re-dispatches each as a fresh single-URL crawl order under its
// recorded profile.
type recrawlSweeper struct {
	frontier  *recrawlfrontier.Frontier
	publisher recrawlPublisher
	initiator yagomodel.Hash
	mint      func() []byte
	now       func() time.Time
	batch     int
}

var newRecrawlSweepTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runRecrawlSweepLoop(ctx context.Context, sweeper recrawlSweeper) {
	sweeper.sweepOnce(ctx)

	ticks, stop := newRecrawlSweepTicks(defaultRecrawlSweepInterval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweeper.sweepOnce(ctx)
		}
	}
}

func (s recrawlSweeper) sweepOnce(ctx context.Context) {
	now := s.now()
	due, err := s.frontier.ClaimDue(ctx, now, s.batch)
	if err != nil {
		slog.ErrorContext(ctx, msgRecrawlSweepClaimFailed, slog.Any("error", err))

		return
	}
	dispatched := 0
	for _, item := range due {
		if s.redispatch(ctx, item, now) {
			dispatched++
		}
	}
	if dispatched > 0 {
		slog.DebugContext(ctx, msgRecrawlSwept, slog.Int("urls", dispatched))
	}
}

func (s recrawlSweeper) redispatch(
	ctx context.Context,
	item recrawlfrontier.DueURL,
	now time.Time,
) bool {
	profile, found, err := s.frontier.ProfileByHandle(ctx, item.ProfileHandle)
	if err != nil {
		slog.ErrorContext(ctx, msgRecrawlSweepClaimFailed,
			slog.String("url", item.URL), slog.Any("error", err))

		return false
	}
	if !found {
		slog.WarnContext(ctx, msgRecrawlSweepProfileMissing,
			slog.String("url", item.URL), slog.String("profile", item.ProfileHandle))

		return false
	}
	order := yagocrawlcontract.CrawlOrder{
		Provenance: s.mint(),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           item.URL,
			Mode:          yagocrawlcontract.CrawlRequestModeURL,
			ProfileHandle: item.ProfileHandle,
			Initiator:     s.initiator,
			AppDate:       now,
		}},
	}
	if err := s.publisher.Publish(ctx, order); err != nil {
		slog.ErrorContext(ctx, msgRecrawlSweepPublishFailed,
			slog.String("url", item.URL), slog.Any("error", err))

		return false
	}

	return true
}
