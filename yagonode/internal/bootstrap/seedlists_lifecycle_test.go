package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type staticSeedlistFetcher map[string][]yagomodel.Seed

const seedlistAggregateFixtureEntries = 2500

type seedlistFetcherFunc func(context.Context, string) ([]yagomodel.Seed, error)

func (f seedlistFetcherFunc) Fetch(
	ctx context.Context,
	url string,
) ([]yagomodel.Seed, error) {
	return f(ctx, url)
}

func (f staticSeedlistFetcher) Fetch(
	ctx context.Context,
	url string,
) ([]yagomodel.Seed, error) {
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("fetch seedlist: %w", cause)
	}

	return f[url], nil
}

func TestSeedlistsUseOneBoundedConcurrentDeadline(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			<-req.Context().Done()

			return nil, req.Context().Err()
		}),
	}
	observer := &recordingSeedImportObserver{}
	source := NewObserved(client, []string{
		"http://one.test/seeds",
		"http://two.test/seeds",
		"http://three.test/seeds",
		"http://four.test/seeds",
	}, observer).(*seedlists)
	source.timeout = 40 * time.Millisecond
	source.concurrency = 2
	started := time.Now()
	seeds := source.Fetch(t.Context())
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("aggregate seedlist timeout = %s", elapsed)
	}
	if len(seeds) != 0 || calls.Load() != 2 || observer.imports != 0 {
		t.Fatalf("seeds/calls/imports = %d/%d/%d", len(seeds), calls.Load(), observer.imports)
	}
}

func TestSeedlistsBoundConcurrentWorkers(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		current := active.Add(1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		started <- struct{}{}
		<-release
		active.Add(-1)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1"),
			)),
		}, nil
	})}
	source := New(client, []string{
		"http://one.test/seeds",
		"http://two.test/seeds",
		"http://three.test/seeds",
		"http://four.test/seeds",
	}).(*seedlists)
	source.concurrency = 2
	done := make(chan []yagomodel.Seed, 1)
	go func() { done <- source.Fetch(t.Context()) }()
	<-started
	<-started
	select {
	case <-started:
		t.Fatal("third seedlist fetch started before a worker was released")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if seeds := <-done; len(seeds) != 1 {
		t.Fatalf("deduplicated seeds = %d", len(seeds))
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrent fetches = %d", maximum.Load())
	}
}

func TestSeedlistsUseDefaultsAndHonorCallerCancellation(t *testing.T) {
	defaultSource := &seedlists{
		fetcher: staticSeedlistFetcher{
			"default": {{Hash: yagomodel.Hash("AAAAAAAAAAAA")}},
		},
		urls: []string{"default"},
	}
	if seeds := defaultSource.Fetch(t.Context()); len(seeds) != 1 {
		t.Fatalf("default refresh seeds = %d", len(seeds))
	}

	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	canceledSource := &seedlists{
		fetcher: seedlistFetcherFunc(func(
			context.Context,
			string,
		) ([]yagomodel.Seed, error) {
			close(started)
			<-release

			return nil, nil
		}),
		urls: []string{"canceled"},
	}
	done := make(chan []yagomodel.Seed, 1)
	go func() { done <- canceledSource.Fetch(ctx) }()
	<-started
	cancel()
	if seeds := <-done; seeds != nil {
		t.Fatalf("canceled refresh seeds = %#v", seeds)
	}
	close(release)
}

func TestSeedlistsPreserveFreshnessDeduplicateAndRejectStaleDates(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	bodies := map[string]string{
		"one.test":     seedlistTimedLine(t, "AAAAAAAAAAAA", "203.0.113.1", now.Add(-2*time.Hour)),
		"two.test":     seedlistTimedLine(t, "AAAAAAAAAAAA", "203.0.113.2", now.Add(-time.Hour)),
		"stale.test":   seedlistTimedLine(t, "BBBBBBBBBBBB", "203.0.113.3", now.Add(-25*time.Hour)),
		"future.test":  seedlistTimedLine(t, "CCCCCCCCCCCC", "203.0.113.4", now.Add(time.Second)),
		"unknown.test": seedlistLine(t, "DDDDDDDDDDDD", "203.0.113.5"),
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(bodies[req.URL.Host])),
			}, nil
		}),
	}
	source := New(client, []string{
		"http://one.test/seeds",
		"http://two.test/seeds",
		"http://stale.test/seeds",
		"http://future.test/seeds",
		"http://unknown.test/seeds",
	}).(*seedlists)
	source.now = func() time.Time { return now }
	seeds := source.Fetch(t.Context())
	if len(seeds) != 2 || seeds[0].Hash != "AAAAAAAAAAAA" ||
		seeds[1].Hash != "DDDDDDDDDDDD" {
		t.Fatalf("filtered seeds = %#v", seeds)
	}
	address, _ := seeds[0].NetworkAddress()
	seen, known := seeds[0].LastSeen.Get()
	if address != "203.0.113.2:8090" || !known || !seen.Time().Equal(now.Add(-time.Hour)) {
		t.Fatalf("newest duplicate = %#v", seeds[0])
	}
}

func TestSeedlistsRetainFastSourceWhenAggregateDeadlineCancelsSlowSource(t *testing.T) {
	slowCanceled := make(chan struct{})
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "slow.test" {
				<-req.Context().Done()
				close(slowCanceled)

				return nil, req.Context().Err()
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					seedlistLine(t, "AAAAAAAAAAAA", "203.0.113.1"),
				)),
			}, nil
		}),
	}
	source := New(client, []string{
		"http://fast.test/seeds",
		"http://slow.test/seeds",
	}).(*seedlists)
	source.timeout = 40 * time.Millisecond
	source.concurrency = 2
	started := time.Now()
	seeds := source.Fetch(t.Context())
	if len(seeds) != 1 || seeds[0].Hash != yagomodel.Hash("AAAAAAAAAAAA") {
		t.Fatalf("partial aggregate seeds = %#v", seeds)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("partial aggregate took %s", elapsed)
	}
	select {
	case <-slowCanceled:
	case <-time.After(time.Second):
		t.Fatal("slow seedlist request did not observe aggregate cancellation")
	}
}

func TestSeedlistsBoundWholeRefreshAcrossMultipleSources(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	urls := []string{"first", "second"}
	fetcher := staticSeedlistFetcher{
		urls[0]: aggregateFixtureSeeds(t, 0, now.Add(-time.Hour), 8<<10),
		urls[1]: aggregateFixtureSeeds(t, seedlistAggregateFixtureEntries, now, 8<<10),
	}
	source := &seedlists{
		fetcher:     fetcher,
		urls:        urls,
		now:         func() time.Time { return now },
		timeout:     time.Second,
		concurrency: 2,
	}
	seeds := source.Fetch(t.Context())
	retainedBytes := 0
	for _, seed := range seeds {
		retainedBytes += seed.RetainedBytes()
	}
	if len(seeds) == 0 || len(seeds) > seedlistMaxEntries ||
		retainedBytes > seedlistMaxRetainedBytes {
		t.Fatalf("aggregate seeds/bytes = %d/%d", len(seeds), retainedBytes)
	}
	if len(seeds) >= 5000 {
		t.Fatalf("whole-refresh byte bound retained every seed: %d", len(seeds))
	}
	if seen, known := seeds[0].LastSeen.Get(); !known || !seen.Time().Equal(now) {
		t.Fatalf("freshest retained seed = %#v", seeds[0])
	}
}

func TestSeedlistsBoundWholeRefreshSeedCountAcrossSources(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	source := &seedlists{
		fetcher: staticSeedlistFetcher{
			"first":  aggregateFixtureSeeds(t, 0, now.Add(-time.Hour), 0),
			"second": aggregateFixtureSeeds(t, seedlistAggregateFixtureEntries, now, 0),
		},
		urls:        []string{"first", "second"},
		now:         func() time.Time { return now },
		timeout:     time.Second,
		concurrency: 2,
	}
	seeds := source.Fetch(t.Context())
	if len(seeds) != seedlistMaxEntries {
		t.Fatalf("whole-refresh seed count = %d", len(seeds))
	}
	if seen, known := seeds[0].LastSeen.Get(); !known || !seen.Time().Equal(now) {
		t.Fatalf("freshest count-bounded seed = %#v", seeds[0])
	}
}

func TestSeedAggregateRejectsSeedLargerThanAggregateBudget(t *testing.T) {
	aggregate := newSeedAggregate()
	aggregate.admit(
		yagomodel.Seed{
			Hash: yagomodel.Hash("AAAAAAAAAAAA"),
			News: yagomodel.Some(strings.Repeat("x", seedlistMaxRetainedBytes)),
		},
		0,
		0,
		time.Now(),
	)
	if aggregate.Len() != 0 || aggregate.retainedBytes != 0 {
		t.Fatalf(
			"oversized seed retained: entries=%d bytes=%d",
			aggregate.Len(),
			aggregate.retainedBytes,
		)
	}
}

func TestSeedAggregateRetainsNewestThenEarliestDuplicate(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	newest := yagomodel.Seed{
		Hash:     yagomodel.Hash("AAAAAAAAAAAA"),
		Name:     yagomodel.Some("newest"),
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now)),
	}
	older := yagomodel.Seed{
		Hash:     newest.Hash,
		Name:     yagomodel.Some("older"),
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now.Add(-time.Hour))),
	}
	replacementAggregate := newSeedAggregate()
	replacementAggregate.admit(older, 0, 0, now)
	replacementAggregate.admit(newest, 1, 0, now)
	replacement := replacementAggregate.result()
	if len(replacement) != 1 {
		t.Fatalf("replacement duplicates = %#v", replacement)
	}
	replacementName, replacementKnown := replacement[0].Name.Get()
	if !replacementKnown || replacementName != "newest" {
		t.Fatalf("replacement duplicate = %#v", replacement)
	}

	aggregate := newSeedAggregate()
	aggregate.admit(newest, 0, 0, now)
	aggregate.admit(older, 0, 1, now)
	aggregate.admit(yagomodel.Seed{
		Hash:     newest.Hash,
		Name:     yagomodel.Some("later-duplicate"),
		LastSeen: newest.LastSeen,
	}, 0, 1, now)
	seeds := aggregate.result()
	if len(seeds) != 1 {
		t.Fatalf("retained duplicates = %#v", seeds)
	}
	name, known := seeds[0].Name.Get()
	if !known || name != "newest" {
		t.Fatalf("retained duplicate = %#v", seeds)
	}
}

func aggregateFixtureSeeds(
	t *testing.T,
	start int,
	seen time.Time,
	newsBytes int,
) []yagomodel.Seed {
	t.Helper()
	host, err := yagomodel.ParseHost("203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	seeds := make([]yagomodel.Seed, seedlistAggregateFixtureEntries)
	for position := range seeds {
		seed := yagomodel.Seed{
			Hash:     yagomodel.Hash(fmt.Sprintf("%012d", start+position)),
			IP:       yagomodel.Some(host),
			Port:     yagomodel.Some(yagomodel.Port(8090)),
			LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(seen)),
		}
		if newsBytes > 0 {
			seed.News = yagomodel.Some(strings.Repeat("x", newsBytes))
		}
		seeds[position] = seed
	}

	return seeds
}

func seedlistTimedLine(t *testing.T, hash, ip string, seen time.Time) string {
	t.Helper()
	host, err := yagomodel.ParseHost(ip)
	if err != nil {
		t.Fatal(err)
	}
	seed := yagomodel.Seed{
		Hash:     yagomodel.Hash(hash),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(yagomodel.Port(8090)),
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(seen)),
	}

	return yagomodel.EncodeCompactWireForm(seed.String())
}
