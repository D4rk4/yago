package frontier

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

func TestWakeDropsWhenSignalAlreadyPending(t *testing.T) {
	frontier := &Frontier{signal: make(chan struct{}, 1)}

	frontier.wake()
	frontier.wake()

	if got := len(frontier.signal); got != 1 {
		t.Fatalf("pending signals = %d, want 1", got)
	}
}

func TestAlwaysDuePaceVisitedNoop(t *testing.T) {
	alwaysDuePace{}.Visited(crawljob.CrawlJob{}, time.Now())
}

func TestDispatchDueHonorsLaterRetryDeadline(t *testing.T) {
	frontier := NewFrontier(1, alwaysDuePace{})
	now := time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC)
	retryAfter := now.Add(time.Minute)
	frontier.mu.Lock()
	due := frontier.dispatchDueLocked(crawljob.CrawlJob{RetryAfter: retryAfter}, now)
	frontier.mu.Unlock()
	if !due.Equal(retryAfter) {
		t.Fatalf("dispatch due = %v, want retry deadline %v", due, retryAfter)
	}
}

func TestFrontierStateAcceptRejectsUnknownProfile(t *testing.T) {
	state := &frontierState{
		runs: map[uuid.UUID]*crawlRun{},
	}

	accepted, _ := state.accept(context.Background(), uuid.New(), frontierCandidate{
		normURL:       "https://example.com/",
		profileHandle: "missing",
	})
	if accepted {
		t.Fatal("unknown profile should not be accepted")
	}
}
