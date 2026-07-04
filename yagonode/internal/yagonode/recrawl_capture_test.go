package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

type capturingQueue struct {
	calls int
	key   string
	order yagocrawlcontract.CrawlOrder
}

func (q *capturingQueue) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.calls++
	q.key = key
	q.order = order

	return false, nil
}

func openTestFrontier(t *testing.T) *recrawlfrontier.Frontier {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	frontier, err := recrawlfrontier.Open(v)
	if err != nil {
		t.Fatalf("open frontier: %v", err)
	}

	return frontier
}

func TestProfileRecordingQueueRecordsProfileThenPublishes(t *testing.T) {
	frontier := openTestFrontier(t)
	inner := &capturingQueue{}
	queue := profileRecordingQueue{inner: inner, frontier: frontier}

	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Example",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		RecrawlIfOlder:  time.Hour,
	})
	order := yagocrawlcontract.CrawlOrder{Provenance: []byte("p"), Profile: profile}

	ctx := context.Background()
	if _, err := queue.PublishOnce(ctx, "key-1", order); err != nil {
		t.Fatalf("publish once: %v", err)
	}

	if inner.calls != 1 || inner.key != "key-1" {
		t.Fatalf("inner queue calls=%d key=%q, want 1/key-1", inner.calls, inner.key)
	}
	got, found, err := frontier.ProfileByHandle(ctx, profile.Handle)
	if err != nil {
		t.Fatalf("profile by handle: %v", err)
	}
	if !found || got.Handle != profile.Handle {
		t.Fatalf("profile not recorded: found=%v got=%+v", found, got)
	}
}

func TestProfileRecordingQueuePublishesDespiteRecordFailure(t *testing.T) {
	frontier := openTestFrontier(t)
	inner := &capturingQueue{}
	queue := profileRecordingQueue{inner: inner, frontier: frontier}

	// An empty-handle profile makes RecordProfile fail, but the order must still
	// reach the inner queue — recording is best-effort.
	order := yagocrawlcontract.CrawlOrder{Profile: yagocrawlcontract.CrawlProfile{}}
	if _, err := queue.PublishOnce(context.Background(), "", order); err != nil {
		t.Fatalf("publish once: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls=%d, want 1 despite record failure", inner.calls)
	}
}
