package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/adminui"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
)

type stubOrderQueue struct {
	order     yacycrawlcontract.CrawlOrder
	key       string
	duplicate bool
	err       error
	called    bool
}

func (q *stubOrderQueue) PublishOnce(
	_ context.Context,
	key string,
	order yacycrawlcontract.CrawlOrder,
) (bool, error) {
	q.called = true
	q.key = key
	q.order = order

	return q.duplicate, q.err
}

func testDispatcher(queue crawldispatch.CrawlOrderQueue) *crawldispatch.Dispatcher {
	var initiator yacymodel.Hash

	return crawldispatch.NewDispatcher(
		initiator,
		func() []byte { return make([]byte, yacymodel.HashLength) },
		queue,
	)
}

func TestCrawlSourceStartDispatches(t *testing.T) {
	queue := &stubOrderQueue{}
	source := newCrawlSource(testDispatcher(queue))

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
	if queue.order.Profile.MaxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost {
		t.Fatalf("maxPagesPerHost = %d, want unlimited", queue.order.Profile.MaxPagesPerHost)
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
