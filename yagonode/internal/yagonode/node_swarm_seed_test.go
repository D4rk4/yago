package yagonode

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type recordingSeeder struct {
	mu   sync.Mutex
	urls []string
}

func (s *recordingSeeder) Seed(_ context.Context, urls []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, urls...)
}

type countingDirectory struct {
	count int
	err   error
}

func (d countingDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, nil
}

func (d countingDirectory) Count(context.Context) (int, error) {
	return d.count, d.err
}

func syncSeedingSearcher(
	inner searchcore.Searcher,
	seeder urlSeeder,
) swarmSeedingSearcher {
	searcher, ok := withSwarmSeedCrawl(inner, seeder).(swarmSeedingSearcher)
	if !ok {
		panic("unexpected searcher type")
	}
	searcher.spawn = func(work func()) { work() }

	return searcher
}

type nullCrawlQueue struct{}

func (nullCrawlQueue) PublishOnce(
	context.Context,
	string,
	yagocrawlcontract.CrawlOrder,
) (bool, error) {
	return false, nil
}

type capturingCrawlQueue struct {
	orders []yagocrawlcontract.CrawlOrder
}

func (q *capturingCrawlQueue) PublishOnce(
	_ context.Context,
	_ string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.orders = append(q.orders, order)

	return true, nil
}

// TestNewCrawlSeederAppliesAutocrawlerProfileBounds proves the tunable
// autocrawler depth and page cap reach the published crawl order rather than
// the former hardcoded depth 1 / 20 pages.
func TestNewCrawlSeederAppliesAutocrawlerProfileBounds(t *testing.T) {
	queue := &capturingCrawlQueue{}
	seeder := newCrawlSeeder(
		queue,
		countingDirectory{},
		nodeidentity.Identity{}.Hash,
		seedProfile{name: swarmSeedProfileName, depth: 3, maxPages: 75},
	)
	seeder.Seed(t.Context(), []string{"https://discovered.example/page"})

	if len(queue.orders) != 1 {
		t.Fatalf("published %d orders, want 1", len(queue.orders))
	}
	profile := queue.orders[0].Profile
	if profile.MaxDepth != 3 || profile.MaxPagesPerHost != 75 {
		t.Fatalf("autocrawler profile bounds = depth %d / %d pages, want 3 / 75",
			profile.MaxDepth, profile.MaxPagesPerHost)
	}
}

func TestNodePublicSearchInstallsSwarmSeedCrawl(t *testing.T) {
	searcher, _ := mountNodePublicSearch(http.NewServeMux(), publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:  nodeidentity.Identity{NetworkName: "freeworld"},
		dht:       defaultPublicSearchDHTConfig(),
		client:    http.DefaultClient,
		seedQueue: nullCrawlQueue{},
		swarmSeed: swarmSeedConfig{
			Enabled:      true,
			SeedDepth:    2,
			SeedMaxPages: 40,
		},
	})

	if _, ok := searcher.(swarmSeedingSearcher); !ok {
		t.Fatalf(
			"searcher = %T, want a swarmSeedingSearcher when greedy learning is enabled",
			searcher,
		)
	}
}

func TestSwarmSeedCrawlSpawnsSeedingOffTheRequestPath(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
		{URL: "https://remote.example/doc", Source: searchcore.SourceRemote},
	}}}
	seeder := &signalingSeeder{done: make(chan struct{})}
	searcher := withSwarmSeedCrawl(inner, seeder)

	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "go"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	select {
	case <-seeder.done:
	case <-time.After(5 * time.Second):
		t.Fatal("seeding goroutine never ran")
	}
}

type signalingSeeder struct {
	done chan struct{}
}

func (s *signalingSeeder) Seed(context.Context, []string) {
	close(s.done)
}

func TestSwarmSeedCrawlSeedsRemoteResultURLs(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
		{URL: "https://remote.example/doc", Source: searchcore.SourceRemote},
		{URL: "https://local.example/doc", Source: searchcore.SourceLocal},
	}}}
	seeder := &recordingSeeder{}
	searcher := syncSeedingSearcher(inner, seeder)

	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "go"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(seeder.urls) != 1 || seeder.urls[0] != "https://remote.example/doc" {
		t.Fatalf("seeded urls = %#v, want only the remote result", seeder.urls)
	}
}

// TestSwarmSeedCrawlSeedsRegardlessOfIndexSize proves greedy learning no longer
// has a document-count ceiling: a large local index still seeds discovered URLs
// so growth never self-throttles.
func TestSwarmSeedCrawlSeedsRegardlessOfIndexSize(t *testing.T) {
	inner := &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
		{URL: "https://remote.example/doc", Source: searchcore.SourceRemote},
	}}}
	seeder := &recordingSeeder{}
	searcher := syncSeedingSearcher(inner, seeder)
	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "go"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(seeder.urls) != 1 {
		t.Fatalf("seeded urls = %#v, want the remote result even with a full index", seeder.urls)
	}
}

func TestSwarmSeedCrawlSkipsLocalOnlyResponsesAndErrors(t *testing.T) {
	seeder := &recordingSeeder{}
	localOnly := &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
		{URL: "https://local.example/doc", Source: searchcore.SourceLocal},
	}}}
	searcher := syncSeedingSearcher(localOnly, seeder)
	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "go"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(seeder.urls) != 0 {
		t.Fatalf("seeded urls = %#v, want none for local-only results", seeder.urls)
	}

	failing := &fakeSearcher{err: errors.New("search down")}
	searcher = syncSeedingSearcher(failing, seeder)
	if _, err := searcher.Search(t.Context(), searchcore.Request{Query: "go"}); err == nil {
		t.Fatal("expected search error to pass through")
	}
	if len(seeder.urls) != 0 {
		t.Fatalf("seeded urls = %#v, want none on search error", seeder.urls)
	}
}
