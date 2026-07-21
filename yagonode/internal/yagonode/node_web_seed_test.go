package yagonode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type fakeCrawlQueue struct {
	mutex     sync.Mutex
	keys      []string
	orders    []yagocrawlcontract.CrawlOrder
	published chan struct{}
}

func (q *fakeCrawlQueue) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.mutex.Lock()
	q.keys = append(q.keys, key)
	q.orders = append(q.orders, order)
	q.mutex.Unlock()
	if q.published != nil {
		q.published <- struct{}{}
	}

	return false, nil
}

func (q *fakeCrawlQueue) snapshot() ([]string, []yagocrawlcontract.CrawlOrder) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return append(
			[]string(nil),
			q.keys...), append(
			[]yagocrawlcontract.CrawlOrder(nil),
			q.orders...)
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

type waitingSeedDocuments struct {
	finished chan error
}

func (d waitingSeedDocuments) Document(
	ctx context.Context,
	_ string,
) (documentstore.Document, bool, error) {
	<-ctx.Done()
	d.finished <- ctx.Err()

	return documentstore.Document{}, false, fmt.Errorf("presence lookup: %w", ctx.Err())
}

func (waitingSeedDocuments) Count(context.Context) (int, error) {
	return 0, nil
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

	keys, orders := queue.snapshot()
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	if keys[0] != "https://fresh.example/page" {
		t.Errorf("keys = %#v, want one discovery URL", keys)
	}
	order := orders[0]
	if len(order.Requests) != 1 || order.Requests[0].URL != "https://fresh.example/page" {
		t.Fatalf("requests = %#v", order.Requests)
	}
	if order.Profile.Name != webSeedProfileName || order.Profile.MaxDepth != 1 {
		t.Errorf("profile = %#v", order.Profile)
	}
	if order.Profile.MaxPagesPerRun == nil ||
		*order.Profile.MaxPagesPerRun != 20 {
		t.Fatalf("max pages per run = %v", order.Profile.MaxPagesPerRun)
	}
	if order.Requests[0].Mode != yagocrawlcontract.CrawlRequestModeURL {
		t.Errorf("mode = %v", order.Requests[0].Mode)
	}
}

func TestWebCrawlSeederBoundsPresenceLookupBeforePublishing(t *testing.T) {
	finished := make(chan error, 1)
	queue := &fakeCrawlQueue{published: make(chan struct{}, 1)}
	seeder := newWebCrawlSeeder(
		queue,
		waitingSeedDocuments{finished: finished},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{
			fallback: webFallbackConfig{SeedDepth: 0, SeedMaxPages: 1},
		},
	)
	started := time.Now()
	seeder.Seed(context.Background(), []string{"https://fresh.example/page"})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("seed elapsed = %v, want at most 1s", elapsed)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("presence lookup error = %v", err)
		}
	default:
		t.Fatal("presence lookup did not observe its deadline")
	}
	select {
	case <-queue.published:
	default:
		t.Fatal("unknown URL was not published after bounded lookup")
	}
}

func TestWebCrawlSeederReadsCurrentRunBudget(t *testing.T) {
	queue := &fakeCrawlQueue{}
	maximum := 12
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
	maximum = 7
	seeder.Seed(context.Background(), []string{"https://two.example/"})

	_, orders := queue.snapshot()
	if len(orders) != 2 {
		t.Fatalf("orders = %d, want 2", len(orders))
	}
	for index, want := range []int{12, 7} {
		profile := orders[index].Profile
		if profile.MaxPagesPerRun == nil || *profile.MaxPagesPerRun != want {
			t.Fatalf("order %d max pages per run = %v, want %d",
				index, profile.MaxPagesPerRun, want)
		}
	}
	if orders[0].Profile.Handle == orders[1].Profile.Handle {
		t.Fatalf("different run budgets shared profile handle %q", orders[0].Profile.Handle)
	}
}

type canceledWebSeedQueue struct {
	calls int
}

func (q *canceledWebSeedQueue) PublishOnce(
	context.Context,
	string,
	yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.calls++

	return false, fmt.Errorf("publish failed")
}

func TestWebSeedPublishRetryStopsWhenContextIsCanceled(t *testing.T) {
	queue := &canceledWebSeedQueue{}
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{
			fallback: webFallbackConfig{SeedDepth: 0, SeedMaxPages: 1},
		},
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	seeder.Seed(ctx, []string{"https://fresh.example/page"})
	if queue.calls != 1 {
		t.Fatalf("publish attempts = %d, want 1 after cancellation", queue.calls)
	}
}

func TestAutomaticDiscoveryPageLimitKeepsTaskCapWhenCrawlerIsUnlimited(t *testing.T) {
	t.Parallel()

	for _, crawlerMaximum := range []int{0, 20, 50000} {
		if got := automaticDiscoveryPageLimit(20, crawlerMaximum); got != 20 {
			t.Fatalf("limit with crawler maximum %d = %d, want 20", crawlerMaximum, got)
		}
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
	for _, raw := range []string{
		"", "ftp://x/", "mailto:a@b", "/relative", "   ",
		"https://user:password@example.test/page", "https:///missing-host",
		"https://example.test/" + strings.Repeat("x", yagomodel.MaximumURLIdentityBytes),
	} {
		if got := seedURL(raw); got != "" {
			t.Errorf("seedURL(%q) = %q, want empty", raw, got)
		}
	}
	if got := seedURL("  https://ok.example/a?b=1#frag  "); got != "https://ok.example/a?b=1" {
		t.Errorf("seedURL trimmed = %q", got)
	}
}
