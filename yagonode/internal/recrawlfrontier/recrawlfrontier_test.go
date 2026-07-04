package recrawlfrontier

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

var testBase = time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

func openTestFrontier(t *testing.T) *Frontier {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	frontier, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	return frontier
}

type observation struct {
	url      string
	handle   string
	interval time.Duration
	at       time.Time
}

func mustObserve(t *testing.T, f *Frontier, obs observation) {
	t.Helper()
	if err := f.Observe(
		context.Background(),
		obs.url,
		obs.handle,
		obs.interval,
		obs.at,
	); err != nil {
		t.Fatalf("observe %s: %v", obs.url, err)
	}
}

func claim(t *testing.T, f *Frontier, now time.Time, limit int) []DueURL {
	t.Helper()
	due, err := f.ClaimDue(context.Background(), now, limit)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}

	return due
}

func TestClaimDueReturnsScheduledURLWhenDue(t *testing.T) {
	f := openTestFrontier(t)
	mustObserve(
		t,
		f,
		observation{
			url:      "https://a.example/",
			handle:   "handle-a",
			interval: time.Hour,
			at:       testBase,
		},
	)

	if due := claim(t, f, testBase.Add(30*time.Minute), 10); len(due) != 0 {
		t.Fatalf("claimed %d before due, want 0", len(due))
	}
	due := claim(t, f, testBase.Add(time.Hour), 10)
	if len(due) != 1 || due[0].URL != "https://a.example/" || due[0].ProfileHandle != "handle-a" {
		t.Fatalf("claimed = %+v, want a.example/handle-a", due)
	}
}

func TestClaimDueAdvancesSoNotReclaimed(t *testing.T) {
	f := openTestFrontier(t)
	interval := time.Hour
	mustObserve(
		t,
		f,
		observation{url: "https://a.example/", handle: "h", interval: interval, at: testBase},
	)

	now := testBase.Add(interval)
	if due := claim(t, f, now, 10); len(due) != 1 {
		t.Fatalf("first claim = %d, want 1", len(due))
	}
	if due := claim(t, f, now, 10); len(due) != 0 {
		t.Fatalf("second claim = %d, want 0 (advanced past now)", len(due))
	}
	if due := claim(t, f, now.Add(interval), 10); len(due) != 1 {
		t.Fatalf("third claim after another interval = %d, want 1", len(due))
	}
}

func TestClaimDueReturnsSoonestFirstWithinLimit(t *testing.T) {
	f := openTestFrontier(t)
	mustObserve(
		t,
		f,
		observation{
			url:      "https://late.example/",
			handle:   "h",
			interval: 3 * time.Hour,
			at:       testBase,
		},
	)
	mustObserve(
		t,
		f,
		observation{
			url:      "https://early.example/",
			handle:   "h",
			interval: 1 * time.Hour,
			at:       testBase,
		},
	)
	mustObserve(
		t,
		f,
		observation{
			url:      "https://mid.example/",
			handle:   "h",
			interval: 2 * time.Hour,
			at:       testBase,
		},
	)

	due := claim(t, f, testBase.Add(10*time.Hour), 2)
	if len(due) != 2 ||
		due[0].URL != "https://early.example/" ||
		due[1].URL != "https://mid.example/" {
		t.Fatalf("claimed = %+v, want early then mid", due)
	}
}

func TestObserveZeroIntervalDropsSchedule(t *testing.T) {
	f := openTestFrontier(t)
	mustObserve(
		t,
		f,
		observation{url: "https://a.example/", handle: "h", interval: time.Hour, at: testBase},
	)
	mustObserve(
		t,
		f,
		observation{url: "https://a.example/", handle: "h", interval: 0, at: testBase},
	)

	if due := claim(t, f, testBase.Add(100*time.Hour), 10); len(due) != 0 {
		t.Fatalf("claimed %d after unscheduling, want 0", len(due))
	}
}

func TestObserveReschedulesWithoutOrphan(t *testing.T) {
	f := openTestFrontier(t)
	interval := time.Hour
	mustObserve(
		t,
		f,
		observation{url: "https://a.example/", handle: "h", interval: interval, at: testBase},
	)
	mustObserve(t, f, observation{
		url:      "https://a.example/",
		handle:   "h",
		interval: interval,
		at:       testBase.Add(interval),
	})

	if due := claim(t, f, testBase.Add(interval), 10); len(due) != 0 {
		t.Fatalf("claimed %d at superseded due time, want 0", len(due))
	}
	if due := claim(t, f, testBase.Add(2*interval), 10); len(due) != 1 {
		t.Fatalf("claimed %d at rescheduled due time, want 1", len(due))
	}
}

func TestFrontierPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	v, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault: %v", err)
	}
	frontier, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mustObserve(
		t,
		frontier,
		observation{
			url:      "https://a.example/",
			handle:   "handle-a",
			interval: time.Hour,
			at:       testBase,
		},
	)
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restored, err := Open(reopened)
	if err != nil {
		t.Fatalf("reopen frontier: %v", err)
	}
	due := claim(t, restored, testBase.Add(2*time.Hour), 10)
	if len(due) != 1 || due[0].URL != "https://a.example/" {
		t.Fatalf("claimed after reopen = %+v, want a.example", due)
	}
}

func TestClaimDueWithNonPositiveLimitReturnsNothing(t *testing.T) {
	f := openTestFrontier(t)
	mustObserve(t, f, observation{
		url:      "https://a.example/",
		handle:   "h",
		interval: time.Hour,
		at:       testBase,
	})
	if due := claim(t, f, testBase.Add(time.Hour), 0); due != nil {
		t.Fatalf("claimed %+v with limit 0, want nil", due)
	}
}

func TestObserveNonPositiveIntervalOnUnknownURLIsNoop(t *testing.T) {
	f := openTestFrontier(t)
	mustObserve(t, f, observation{
		url:      "https://never-seen.example/",
		handle:   "h",
		interval: 0,
		at:       testBase,
	})
	if due := claim(t, f, testBase.Add(100*time.Hour), 10); len(due) != 0 {
		t.Fatalf("claimed %d for never-scheduled url, want 0", len(due))
	}
}
