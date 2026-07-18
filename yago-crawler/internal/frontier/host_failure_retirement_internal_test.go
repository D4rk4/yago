package frontier

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
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

func TestHostOutcomeGenerationOverflowFailsClosed(t *testing.T) {
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier.db"))
	if err != nil {
		t.Fatalf("open host generation checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	profile := internalProfile(t)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.com/page"),
			Provenance:    []byte("host-generation-overflow"),
			OrderIdentity: []byte("host-generation-overflow-order"),
		},
		profile,
		nil,
	)
	job := internalReceive(t, frontier)
	frontier.mu.Lock()
	frontier.state.runs[seeded.RunID].hostGenerations["example.com"] = math.MaxUint64
	frontier.mu.Unlock()
	frontier.RecordHostFetchOutcome(context.Background(), job, true)
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("host generation overflow failure = %v", frontier.CheckpointFailure())
	}
}
