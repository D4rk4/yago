package websearch

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type blockingWebSeeder struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
	deadline atomic.Bool
}

func (s *blockingWebSeeder) Seed(ctx context.Context, _ []string) {
	s.calls.Add(1)
	_, hasDeadline := ctx.Deadline()
	s.deadline.Store(hasDeadline)
	s.started <- struct{}{}
	<-s.release
	s.finished <- struct{}{}
}

func TestWebSeedCrawlDoesNotDelaySearchResponse(t *testing.T) {
	seeder := newBlockingWebSeeder()
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		&stubProvider{results: []Result{{Title: "gap", URL: "https://web.example/gap"}}},
		enabled,
		WithSeeder(seeder),
	)
	searchDone := make(chan error, 1)
	go func() {
		_, err := searcher.Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
		searchDone <- err
	}()

	waitForWebSeedSignal(t, seeder.started, "seeder did not start")
	select {
	case err := <-searchDone:
		if err != nil {
			t.Fatalf("search: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("search waited for crawl seeding")
	}
	if !seeder.deadline.Load() {
		t.Fatal("background seeding has no deadline")
	}
	close(seeder.release)
	waitForWebSeedSignal(t, seeder.finished, "seeder did not finish")
}

func TestWebSeedCrawlDropsWorkWhenAdmissionIsFull(t *testing.T) {
	seeder := newBlockingWebSeeder()
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		&stubProvider{results: []Result{{Title: "gap", URL: "https://web.example/gap"}}},
		enabled,
		WithSeeder(seeder),
	)
	searcher.spawnSeedWork = newWebSeedAdmission(1).try

	if _, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("first search: %v", err)
	}
	waitForWebSeedSignal(t, seeder.started, "first seeder did not start")
	if _, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("saturated search: %v", err)
	}
	if seeder.calls.Load() != 1 {
		t.Fatalf("seeder calls = %d, want 1", seeder.calls.Load())
	}
	close(seeder.release)
	waitForWebSeedSignal(t, seeder.finished, "first seeder did not finish")
}

func newBlockingWebSeeder() *blockingWebSeeder {
	return &blockingWebSeeder{
		started:  make(chan struct{}, 2),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 2),
	}
}

func waitForWebSeedSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}
