package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

type stubOrderQueue struct {
	order     yagocrawlcontract.CrawlOrder
	key       string
	duplicate bool
	err       error
	called    bool
}

func (q *stubOrderQueue) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.called = true
	q.key = key
	q.order = order

	return q.duplicate, q.err
}

func testDispatcher(queue crawldispatch.CrawlOrderQueue) *crawldispatch.Dispatcher {
	var initiator yagomodel.Hash

	return crawldispatch.NewDispatcher(
		initiator,
		func() []byte { return make([]byte, yagomodel.HashLength) },
		queue,
	)
}

func TestCrawlSourceStartDispatches(t *testing.T) {
	queue := &stubOrderQueue{}
	source := newCrawlSource(testDispatcher(queue))
	if got := source.MaxPagesPerRun(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("source max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}

	got, err := source.Start(context.Background(), adminui.CrawlStart{
		Seeds:    []string{"http://a.example", "http://b.example"},
		Mode:     "url",
		Scope:    "domain",
		MaxDepth: 2,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !queue.called {
		t.Fatal("expected the order queue to be published to")
	}
	if got.Seeds != 2 || len(queue.order.Requests) != 2 {
		t.Fatalf("seeds = %d, requests = %d", got.Seeds, len(queue.order.Requests))
	}
	if queue.order.Profile.MaxPagesPerHost != yagocrawlcontract.UnlimitedPagesPerHost {
		t.Fatalf("maxPagesPerHost = %d, want unlimited", queue.order.Profile.MaxPagesPerHost)
	}
	if queue.order.Profile.MaxPagesPerRun == nil ||
		*queue.order.Profile.MaxPagesPerRun != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("maxPagesPerRun = %v", queue.order.Profile.MaxPagesPerRun)
	}
}

func TestCrawlSourceStartAppliesExpertFields(t *testing.T) {
	queue := &stubOrderQueue{}
	source := newCrawlSource(testDispatcher(queue))
	maximum := 900

	if _, err := source.Start(context.Background(), adminui.CrawlStart{
		Seeds:                []string{"http://a.example"},
		Mode:                 "url",
		Scope:                "domain",
		MaxDepth:             2,
		URLMustMatch:         `https?://a\.example/.*`,
		URLMustNotMatch:      `.*\.pdf$`,
		IndexURLMustMatch:    ".*",
		IndexURLMustNotMatch: `.*/private/.*`,
		MaxPagesPerHost:      50,
		MaxPagesPerRun:       &maximum,
		AllowQueryURLs:       true,
		FollowNoFollowLinks:  true,
		RecrawlIfOlder:       "24h",
		CrawlDelay:           "2s",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	profile := queue.order.Profile
	if profile.MaxPagesPerHost != 50 {
		t.Fatalf("maxPagesPerHost = %d, want 50", profile.MaxPagesPerHost)
	}
	if profile.MaxPagesPerRun == nil || *profile.MaxPagesPerRun != 900 {
		t.Fatalf("maxPagesPerRun = %v, want 900", profile.MaxPagesPerRun)
	}
	if profile.URLMustNotMatch != `.*\.pdf$` || profile.IndexURLMustNotMatch != `.*/private/.*` {
		t.Fatalf("regex filters not applied: %+v", profile)
	}
	if !profile.AllowQueryURLs || !profile.FollowNoFollowLinks {
		t.Fatalf("boolean options not applied: %+v", profile)
	}
	if profile.CrawlDelay != 2*time.Second || profile.RecrawlIfOlder != 24*time.Hour {
		t.Fatalf(
			"durations not applied: delay=%v recrawl=%v",
			profile.CrawlDelay,
			profile.RecrawlIfOlder,
		)
	}
}

func TestCrawlSourceStartPublishError(t *testing.T) {
	queue := &stubOrderQueue{err: errors.New("disk full")}
	source := newCrawlSource(testDispatcher(queue))

	if _, err := source.Start(context.Background(), adminui.CrawlStart{
		Seeds: []string{"http://a.example"},
		Mode:  "url",
		Scope: "domain",
	}); err == nil {
		t.Fatal("expected a publish error")
	}
}

func TestCrawlSourceRejectsUnknownScope(t *testing.T) {
	queue := &stubOrderQueue{}
	source := newCrawlSource(testDispatcher(queue))

	if _, err := source.Start(context.Background(), adminui.CrawlStart{
		Seeds: []string{"http://a.example"},
		Mode:  "url",
		Scope: "bogus",
	}); err == nil {
		t.Fatal("expected a validation error for an unknown scope")
	}
	if queue.called {
		t.Fatal("an invalid order must not be published")
	}
}
