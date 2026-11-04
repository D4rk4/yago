package robots

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
)

func TestAllowsSharesConcurrentOriginFetchAndCachesRules(t *testing.T) {
	var fetches atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	policy := NewPolicy(func(ctx context.Context, _ string) (httpfetch.PageResponse, error) {
		if fetches.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return httpfetch.PageResponse{
				StatusCode: 200,
				Body:       []byte("User-agent: *\nAllow: /\n"),
			}, nil
		case <-ctx.Done():
			return httpfetch.PageResponse{}, ctx.Err()
		}
	}, "yago-test", 0)

	const callers = 12
	results := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			allowed, err := policy.Allows(context.Background(), "https://example.org/page", false)
			if err == nil && !allowed {
				err = errors.New("page was denied")
			}
			results <- err
		}()
	}
	<-started
	time.Sleep(20 * time.Millisecond)
	close(release)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("allow page: %v", err)
		}
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("robots fetches = %d, want one shared fetch", got)
	}
	if allowed, err := policy.Allows(context.Background(), "https://example.org/next", false); err != nil || !allowed {
		t.Fatalf("cached rules allowed %t, err %v", allowed, err)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("robots fetches after cache hit = %d, want one", got)
	}
}

func TestAllowsCachesUnavailableRobotsAsDisallowAll(t *testing.T) {
	var fetches atomic.Int32
	policy := NewPolicy(func(context.Context, string) (httpfetch.PageResponse, error) {
		fetches.Add(1)
		return httpfetch.PageResponse{}, context.DeadlineExceeded
	}, "yago-test", 0)

	if allowed, err := policy.Allows(context.Background(), "https://slow.example/page", false); err != nil || allowed {
		t.Fatalf("timed-out robots result allowed %t, err %v", allowed, err)
	}
	if allowed, err := policy.Allows(context.Background(), "https://slow.example/next", false); err != nil || allowed {
		t.Fatalf("cached unavailable robots result allowed %t, err %v", allowed, err)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("robots fetches = %d, want cached unavailable result", got)
	}
}

func TestAllowsDoesNotCacheWhenCrawlAttemptDeadlineExpires(t *testing.T) {
	var fetches atomic.Int32
	policy := NewPolicy(func(ctx context.Context, _ string) (httpfetch.PageResponse, error) {
		if fetches.Add(1) == 1 {
			<-ctx.Done()
			return httpfetch.PageResponse{}, ctx.Err()
		}
		return httpfetch.PageResponse{
			StatusCode: 200,
			Body:       []byte("User-agent: *\nAllow: /\n"),
		}, nil
	}, "yago-test", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if allowed, err := policy.Allows(ctx, "https://deadline.example/page", false); !errors.Is(err, context.DeadlineExceeded) || allowed {
		t.Fatalf("expired crawl attempt allowed %t, err %v", allowed, err)
	}
	if len(policy.cache) != 0 {
		t.Fatal("expired crawl attempt populated the robots cache")
	}
	if allowed, err := policy.Allows(context.Background(), "https://deadline.example/next", false); err != nil || !allowed {
		t.Fatalf("fresh robots result allowed %t, err %v", allowed, err)
	}
	if got := fetches.Load(); got != 2 {
		t.Fatalf("robots fetches = %d, want one retry after the expired crawl attempt", got)
	}
}

func TestAllowsDoesNotCacheWhenCrawlIsCancelled(t *testing.T) {
	var fetches atomic.Int32
	policy := NewPolicy(func(context.Context, string) (httpfetch.PageResponse, error) {
		fetches.Add(1)
		return httpfetch.PageResponse{}, context.Canceled
	}, "yago-test", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if allowed, err := policy.Allows(ctx, "https://cancelled.example/page", false); err == nil || allowed {
		t.Fatalf("cancelled robots result allowed %t, err %v", allowed, err)
	}
	if len(policy.cache) != 0 {
		t.Fatal("cancelled robots request populated the cache")
	}
}
