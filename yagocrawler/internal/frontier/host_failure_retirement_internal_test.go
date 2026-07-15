package frontier

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

func TestRetireReadyHostClearsDroppedJobs(t *testing.T) {
	retiredRun := uuid.New()
	keptRun := uuid.New()
	frontier := NewFrontier(3, nil)
	frontier.state.ready = []crawljob.CrawlJob{
		{RunID: retiredRun, URL: "https://failed.example/one"},
		{RunID: keptRun, URL: "https://healthy.example/one"},
		{RunID: retiredRun, URL: "https://failed.example/two"},
	}
	frontier.readyPerRun[retiredRun] = 2
	frontier.readyPerRun[keptRun] = 1

	if dropped := frontier.retireReadyHostLocked(retiredRun, "failed.example"); dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
	if len(frontier.state.ready) != 1 || frontier.state.ready[0].RunID != keptRun {
		t.Fatalf("ready = %+v", frontier.state.ready)
	}
	if len(frontier.readyPerRun) != 1 || frontier.readyPerRun[keptRun] != 1 {
		t.Fatalf("ready per run = %v", frontier.readyPerRun)
	}
	backing := frontier.state.ready[:3]
	if !reflect.DeepEqual(backing[1], crawljob.CrawlJob{}) ||
		!reflect.DeepEqual(backing[2], crawljob.CrawlJob{}) {
		t.Fatalf("dropped jobs remain referenced: %+v", backing[1:])
	}
}
