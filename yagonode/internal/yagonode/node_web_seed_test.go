package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeCrawlQueue struct {
	keys      []string
	orders    []yagocrawlcontract.CrawlOrder
	published chan struct{}
}

func (q *fakeCrawlQueue) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.keys = append(q.keys, key)
	q.orders = append(q.orders, order)
	if q.published != nil {
		q.published <- struct{}{}
	}

	return false, nil
}

type fakeSeedDocuments struct {
	stored map[string]bool
}

func (d fakeSeedDocuments) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, d.stored[normalizedURL], nil
}

func (d fakeSeedDocuments) Count(context.Context) (int, error) {
	return len(d.stored), nil
}

func TestWebCrawlSeederPublishesUnknownURLs(t *testing.T) {
	queue := &fakeCrawlQueue{}
	docs := fakeSeedDocuments{stored: map[string]bool{"https://known.example/": true}}
	seeder := newWebCrawlSeeder(queue, docs, yagomodel.Hash("node"), webCrawlSeedProfile{
		fallback: webFallbackConfig{SeedDepth: 1, SeedMaxPages: 20},
	})

	seeder.Seed(context.Background(), []string{
		"https://fresh.example/page#frag",
		"https://known.example/",
		"ftp://blocked.example/",
		"   ",
	})

	if len(queue.orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(queue.orders))
	}
	if queue.keys[0] != "https://fresh.example/page" {
		t.Errorf("key = %q, want fragment stripped", queue.keys[0])
	}
	order := queue.orders[0]
	if len(order.Requests) != 1 || order.Requests[0].URL != "https://fresh.example/page" {
		t.Fatalf("requests = %#v", order.Requests)
	}
	if order.Profile.Name != webSeedProfileName || order.Profile.MaxDepth != 1 {
		t.Errorf("profile = %#v", order.Profile)
	}
	if order.Profile.MaxPagesPerRun == nil ||
		*order.Profile.MaxPagesPerRun != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("max pages per run = %v", order.Profile.MaxPagesPerRun)
	}
	if order.Requests[0].Mode != yagocrawlcontract.CrawlRequestModeURL {
		t.Errorf("mode = %v", order.Requests[0].Mode)
	}
}

func TestWebCrawlSeederReadsCurrentRunBudget(t *testing.T) {
	queue := &fakeCrawlQueue{}
	maximum := 123
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{stored: map[string]bool{}},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{
			fallback:       webFallbackConfig{SeedDepth: 1, SeedMaxPages: 20},
			maxPagesPerRun: func() int { return maximum },
		},
	)
	seeder.Seed(context.Background(), []string{"https://one.example/"})
	maximum = 456
	seeder.Seed(context.Background(), []string{"https://two.example/"})

	if len(queue.orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(queue.orders))
	}
	for index, want := range []int{123, 456} {
		profile := queue.orders[index].Profile
		if profile.MaxPagesPerRun == nil || *profile.MaxPagesPerRun != want {
			t.Fatalf("order %d max pages per run = %v, want %d",
				index, profile.MaxPagesPerRun, want)
		}
	}
	if queue.orders[0].Profile.Handle == queue.orders[1].Profile.Handle {
		t.Fatalf("different run budgets shared profile handle %q", queue.orders[0].Profile.Handle)
	}
}

func TestSeedRunBudgetSourceRejectsNegativeValue(t *testing.T) {
	source := selectMaxPagesPerRunSource([]func() int{func() int { return -1 }})
	if got := source(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
}

func TestSeedURLRejectsNonHTTP(t *testing.T) {
	for _, raw := range []string{"", "ftp://x/", "mailto:a@b", "/relative", "   "} {
		if got := seedURL(raw); got != "" {
			t.Errorf("seedURL(%q) = %q, want empty", raw, got)
		}
	}
	if got := seedURL("  https://ok.example/a?b=1#frag  "); got != "https://ok.example/a?b=1" {
		t.Errorf("seedURL trimmed = %q", got)
	}
}
