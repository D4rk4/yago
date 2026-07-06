package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type capturedOrderQueue struct {
	got yagocrawlcontract.CrawlOrder
}

func (q *capturedOrderQueue) PublishOnce(
	_ context.Context,
	_ string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.got = order

	return false, nil
}

func TestFormatStampingQueueStampsSharedToggles(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := crawlformats.Open(v)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	custom := yagocrawlcontract.FormatToggles{PDF: true}
	if err := store.Set(context.Background(), custom); err != nil {
		t.Fatalf("set: %v", err)
	}

	inner := &capturedOrderQueue{}
	queue := formatStampingQueue{inner: inner, formats: store}
	if _, err := queue.PublishOnce(
		context.Background(), "k", yagocrawlcontract.CrawlOrder{},
	); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if inner.got.Profile.Formats != custom {
		t.Fatalf("stamped formats = %+v, want %+v", inner.got.Profile.Formats, custom)
	}
}

func TestCrawlFormatsAdminHidesWithoutRuntime(t *testing.T) {
	if got := crawlFormatsAdmin(nil); got != nil {
		t.Fatalf("nil runtime source = %v, want nil", got)
	}
}
