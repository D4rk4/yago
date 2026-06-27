package crawldelay_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawldelay"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
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
}

func TestHostPaceIndependentHosts(t *testing.T) {
	pace := newPace(t, time.Second)
	now := time.Now()
	pace.Visited(jobFor("https://a.example/x"), now)
	if due := pace.DueAt(jobFor("https://b.example/x"), now); !due.Equal(now) {
		t.Errorf("other host due = %v, want %v", due, now)
	}
}
