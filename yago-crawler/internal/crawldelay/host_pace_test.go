package crawldelay_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

func jobFor(url string) crawljob.CrawlJob {
	return crawljob.CrawlJob{URL: url}
}

func newPace(t *testing.T, delay time.Duration) *crawldelay.HostPace {
	t.Helper()
	pace, err := crawldelay.NewHostPace(delay, 8)
	if err != nil {
		t.Fatalf("new host pace: %v", err)
	}
	return pace
}

func TestHostPaceUnseenHostDueNow(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Now()
	if due := pace.DueAt(jobFor("https://example.com/a"), now); !due.Equal(now) {
		t.Errorf("unseen host due = %v, want %v", due, now)
	}
}

func TestHostPaceVisitDelaysNextDue(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Now()
	pace.Visited(jobFor("https://example.com/a"), now)
	want := now.Add(time.Second)
	if due := pace.DueAt(jobFor("https://example.com/b"), now); !due.Equal(want) {
		t.Errorf("visited host due = %v, want %v", due, want)
	}
	if state := pace.SnapshotHost("https://example.com/b"); state.Generation != 1 {
		t.Fatalf("first host generation = %d, want 1", state.Generation)
	}
}

func TestHostPaceVisitUsesProfileDelayWhenSet(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Now()
	job := crawljob.CrawlJob{URL: "https://example.com/a", CrawlDelay: 5 * time.Second}
	pace.Visited(job, now)
	want := now.Add(5 * time.Second)
	if due := pace.DueAt(jobFor("https://example.com/b"), now); !due.Equal(want) {
		t.Errorf("profile-delay host due = %v, want %v", due, want)
	}
}

func TestHostPaceVisitCannotShortenRestoredHostDeadline(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC)
	pace.RestoreHost("example.com", crawldelayState(now.Add(10*time.Minute)))
	pace.Visited(crawljob.CrawlJob{
		URL:        "https://example.com/a",
		CrawlDelay: 5 * time.Second,
	}, now)
	if due := pace.DueAt(
		jobFor("https://example.com/b"),
		now,
	); !due.Equal(
		now.Add(10 * time.Minute),
	) {
		t.Fatalf("visited host shortened restored due to %v", due)
	}
}

func TestHostPaceVisitFallsBackToGlobalDelay(t *testing.T) {
	pace := newPace(t, 2*time.Second)
	now := time.Now()
	// CrawlDelay zero → the global default applies.
	pace.Visited(jobFor("https://example.com/a"), now)
	want := now.Add(2 * time.Second)
	if due := pace.DueAt(jobFor("https://example.com/b"), now); !due.Equal(want) {
		t.Errorf("global-delay host due = %v, want %v", due, want)
	}
}

func TestHostPaceIndependentHosts(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Now()
	pace.Visited(jobFor("https://a.example/x"), now)
	if due := pace.DueAt(jobFor("https://b.example/x"), now); !due.Equal(now) {
		t.Errorf("other host due = %v, want %v", due, now)
	}
}

func TestHostPaceRestoresLatestCheckpoint(t *testing.T) {
	now := time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC)
	first := newPace(t, time.Second)
	job := jobFor("https://example.com/a")
	first.Visited(job, now)
	state := first.SnapshotHost(job.URL)
	restored := newPace(t, time.Second)
	restored.RestoreHost("example.com", state)
	if due := restored.DueAt(job, now); !due.Equal(now.Add(time.Second)) {
		t.Fatalf("restored due = %v, want %v", due, now.Add(time.Second))
	}
	restored.RestoreHost("example.com", crawldelayState(now.Add(500*time.Millisecond)))
	if due := restored.DueAt(job, now); !due.Equal(now.Add(time.Second)) {
		t.Fatalf("older restore shortened due time to %v", due)
	}
}

func crawldelayState(due time.Time) crawlpace.HostState {
	return crawlpace.HostState{NextDueAt: due, Generation: 1}
}

func TestNewHostPaceRejectsInvalidCacheSize(t *testing.T) {
	if _, err := crawldelay.NewHostPace(time.Second, 0); err == nil {
		t.Fatal("zero host cache size should fail")
	}
}

func TestHostPaceRestoreIgnoresEmptyStateAndPreservesEqualGenerationDeadline(t *testing.T) {
	pace := newPace(t, time.Second)
	if pace.Capacity() != 8 {
		t.Fatalf("capacity = %d, want 8", pace.Capacity())
	}
	pace.RestoreHost("example.com", crawlpace.HostState{})
	pace.RestoreHost("example.com", crawlpace.HostState{Generation: 1})
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	pace.RestoreHost("example.com", crawlpace.HostState{
		NextDueAt:  now.Add(time.Minute),
		Generation: 1,
	})
	pace.RestoreHost("example.com", crawlpace.HostState{
		NextDueAt:  now.Add(time.Second),
		Generation: 1,
	})
	due := pace.DueAt(jobFor("https://example.com/page"), now)
	if !due.Equal(now.Add(time.Minute)) {
		t.Fatalf("equal-generation restore shortened deadline to %v", due)
	}
}
