package frontier

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
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

func TestFrontierStateAcceptRejectsUnknownProfile(t *testing.T) {
	state := &frontierState{
		runs: map[uuid.UUID]*crawlRun{},
	}

	if state.accept(context.Background(), uuid.New(), frontierCandidate{
		normURL:       "https://example.com/",
		profileHandle: "missing",
	}) {
		t.Fatal("unknown profile should not be accepted")
	}
}
