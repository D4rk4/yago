package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, yagocrawlcontract.CrawlOrder) error {
	return errors.New("publish failed")
}

func TestNewRecrawlSweepTicksDefault(t *testing.T) {
	ticks, stop := newRecrawlSweepTicks(time.Minute)
	if ticks == nil {
		t.Fatal("default ticks channel is nil")
	}
	stop()
}

func TestSweepOnceReturnsWhenClaimDueFails(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	frontier, err := recrawlfrontier.Open(v)
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: &capturingPublisher{},
		mint:      func() []byte { return []byte("p") },
		now:       func() time.Time { return sweepBase },
		batch:     16,
	}
	sweeper.sweepOnce(context.Background())

	if orders := (&capturingPublisher{}).snapshot(); len(orders) != 0 {
		t.Fatalf("claim failure should publish nothing")
	}
}

func TestRedispatchSkipsURLWithUnknownProfile(t *testing.T) {
	frontier := openTestFrontier(t)
	ctx := context.Background()
	if err := frontier.Observe(
		ctx, "https://ghost.example/", "ghost-handle", time.Hour, sweepBase,
	); err != nil {
		t.Fatalf("observe: %v", err)
	}
	publisher := &capturingPublisher{}
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		mint:      func() []byte { return []byte("p") },
		now:       func() time.Time { return sweepBase.Add(2 * time.Hour) },
		batch:     16,
	}

	sweeper.sweepOnce(ctx)

	if orders := publisher.snapshot(); len(orders) != 0 {
		t.Fatalf("published %d for orphan profile, want 0", len(orders))
	}
}

func TestRedispatchReportsPublishError(t *testing.T) {
	frontier := openTestFrontier(t)
	seedDueURL(t, frontier, "https://publish-error.example/")
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: failingPublisher{},
		mint:      func() []byte { return []byte("p") },
		now:       func() time.Time { return sweepBase.Add(2 * time.Hour) },
		batch:     16,
	}

	sweeper.sweepOnce(context.Background())
}
