package crawldelay

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

type countingBackoffObserver struct{ backoffs int }

func (o *countingBackoffObserver) ObserveHostBackoff() { o.backoffs++ }

func adaptiveFixture(t *testing.T, observer BackoffObserver) *AdaptivePace {
	t.Helper()
	inner, err := NewHostPace(time.Second, 16)
	if err != nil {
		t.Fatalf("NewHostPace: %v", err)
	}
	pace, err := NewAdaptivePace(inner, 16, observer)
	if err != nil {
		t.Fatalf("NewAdaptivePace: %v", err)
	}

	return pace
}

func TestAdaptivePaceBacksOffExponentiallyAndParks(t *testing.T) {
	observer := &countingBackoffObserver{}
	pace := adaptiveFixture(t, observer)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	job := crawljob.CrawlJob{URL: "https://busy.example/page"}

	pace.Throttled(job.URL, 0, now)
	if due := pace.DueAt(job, now); due.Sub(now) != 2*time.Second {
		t.Fatalf("first backoff = %v, want 2s", due.Sub(now))
	}
	pace.Throttled(job.URL, 0, now)
	if due := pace.DueAt(job, now); due.Sub(now) != 4*time.Second {
		t.Fatalf("second backoff = %v, want 4s", due.Sub(now))
	}
	for range 3 {
		pace.Throttled(job.URL, 0, now)
	}
	if due := pace.DueAt(job, now); due.Sub(now) != maxHostBackoff {
		t.Fatalf("parked backoff = %v, want %v", due.Sub(now), maxHostBackoff)
	}
	if observer.backoffs != 5 {
		t.Fatalf("observer backoffs = %d, want 5", observer.backoffs)
	}
}

func TestAdaptivePaceHonorsAndClampsRetryAfter(t *testing.T) {
	pace := adaptiveFixture(t, nil)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	job := crawljob.CrawlJob{URL: "https://busy.example/page"}

	pace.Throttled(job.URL, time.Minute, now)
	if due := pace.DueAt(job, now); due.Sub(now) != time.Minute {
		t.Fatalf("retry-after backoff = %v, want 1m", due.Sub(now))
	}
	pace.Throttled(job.URL, time.Hour, now)
	if due := pace.DueAt(job, now); due.Sub(now) != maxHostBackoff {
		t.Fatalf("hostile retry-after = %v, want clamp %v", due.Sub(now), maxHostBackoff)
	}
}

func TestAdaptivePaceRecoversOnSuccess(t *testing.T) {
	pace := adaptiveFixture(t, nil)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	job := crawljob.CrawlJob{URL: "https://busy.example/page"}

	pace.Throttled(job.URL, 8*time.Second, now)
	pace.Succeeded(job.URL, now)
	if due := pace.DueAt(job, now); due.Sub(now) != 4*time.Second {
		t.Fatalf("halved backoff = %v, want 4s", due.Sub(now))
	}
	pace.Succeeded(job.URL, now)
	pace.Succeeded(job.URL, now)
	if due := pace.DueAt(job, now); !due.Equal(now) {
		t.Fatalf("recovered host still backed off until %v", due)
	}
	pace.Succeeded(job.URL, now)
}

func TestAdaptivePaceLeavesUntouchedHostsOnFixedPace(t *testing.T) {
	pace := adaptiveFixture(t, nil)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	job := crawljob.CrawlJob{URL: "https://calm.example/page"}

	if due := pace.DueAt(job, now); !due.Equal(now) {
		t.Fatalf("fresh host due = %v, want now", due)
	}
	pace.Visited(job, now)
	if due := pace.DueAt(job, now); due.Sub(now) != time.Second {
		t.Fatalf("fixed pace due = %v, want 1s", due.Sub(now))
	}
}

func TestNewAdaptivePaceRejectsBadCacheSize(t *testing.T) {
	inner, err := NewHostPace(time.Second, 16)
	if err != nil {
		t.Fatalf("NewHostPace: %v", err)
	}
	if _, err := NewAdaptivePace(inner, 0, nil); err == nil {
		t.Fatal("expected cache size error")
	}
}
