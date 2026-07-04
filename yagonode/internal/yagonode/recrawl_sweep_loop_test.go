package yagonode

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

type capturingPublisher struct {
	mu     sync.Mutex
	orders []yagocrawlcontract.CrawlOrder
	signal chan struct{}
}

func (p *capturingPublisher) Publish(
	_ context.Context,
	order yagocrawlcontract.CrawlOrder,
) error {
	p.mu.Lock()
	p.orders = append(p.orders, order)
	p.mu.Unlock()
	if p.signal != nil {
		p.signal <- struct{}{}
	}

	return nil
}

func (p *capturingPublisher) snapshot() []yagocrawlcontract.CrawlOrder {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]yagocrawlcontract.CrawlOrder(nil), p.orders...)
}

var sweepBase = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

func seedDueURL(
	t *testing.T,
	frontier *recrawlfrontier.Frontier,
	url string,
) yagocrawlcontract.CrawlProfile {
	t.Helper()
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Example",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		RecrawlIfOlder:  time.Hour,
	})
	ctx := context.Background()
	if err := frontier.RecordProfile(ctx, profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := frontier.RecordFetch(ctx, url, profile.Handle, sweepBase); err != nil {
		t.Fatalf("record fetch: %v", err)
	}

	return profile
}

func TestSweepOnceRedispatchesDueURL(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := seedDueURL(t, frontier, "https://a.example/")
	publisher := &capturingPublisher{}
	initiator := yagomodel.Hash("initiator")
	dispatchAt := sweepBase.Add(time.Hour)
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		initiator: initiator,
		mint:      func() []byte { return []byte("prov") },
		now:       func() time.Time { return dispatchAt },
		batch:     16,
	}

	sweeper.sweepOnce(context.Background())

	orders := publisher.snapshot()
	if len(orders) != 1 {
		t.Fatalf("published %d orders, want 1", len(orders))
	}
	order := orders[0]
	if order.Profile.Handle != profile.Handle || string(order.Provenance) != "prov" {
		t.Fatalf("order = %+v, want profile %s prov", order, profile.Handle)
	}
	if len(order.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(order.Requests))
	}
	req := order.Requests[0]
	if req.URL != "https://a.example/" ||
		req.Mode != yagocrawlcontract.CrawlRequestModeURL ||
		req.ProfileHandle != profile.Handle ||
		req.Initiator != initiator ||
		!req.AppDate.Equal(dispatchAt) {
		t.Fatalf("request = %+v", req)
	}
}

func TestSweepOnceSkipsNotYetDueURL(t *testing.T) {
	frontier := openTestFrontier(t)
	seedDueURL(t, frontier, "https://a.example/")
	publisher := &capturingPublisher{}
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		mint:      func() []byte { return []byte("p") },
		now:       func() time.Time { return sweepBase.Add(30 * time.Minute) },
		batch:     16,
	}

	sweeper.sweepOnce(context.Background())

	if orders := publisher.snapshot(); len(orders) != 0 {
		t.Fatalf("published %d before due, want 0", len(orders))
	}
}

func TestRecrawlSweepLoopRunsAndStops(t *testing.T) {
	oldTicks := newRecrawlSweepTicks
	t.Cleanup(func() { newRecrawlSweepTicks = oldTicks })
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	newRecrawlSweepTicks = func(time.Duration) (<-chan time.Time, func()) {
		return ticks, func() { stopped.Store(true) }
	}

	frontier := openTestFrontier(t)
	seedDueURL(t, frontier, "https://a.example/")
	var passes atomic.Int64
	publisher := &capturingPublisher{signal: make(chan struct{}, 4)}
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		mint:      func() []byte { return []byte("p") },
		now: func() time.Time {
			return sweepBase.Add(time.Duration(passes.Add(1)) * time.Hour)
		},
		batch: 16,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRecrawlSweepLoop(ctx, sweeper)
		close(done)
	}()

	select {
	case <-publisher.signal:
	case <-time.After(time.Second):
		t.Fatal("immediate sweep did not run")
	}
	ticks <- time.Now()
	select {
	case <-publisher.signal:
	case <-time.After(time.Second):
		t.Fatal("ticked sweep did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop on cancel")
	}
	if !stopped.Load() {
		t.Fatal("ticker stop not called")
	}
}
