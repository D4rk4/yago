package frontier

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestFrontierPageTotalsRejectUnrepresentableValues(t *testing.T) {
	if _, err := platformPageTotal(uint64(math.MaxInt) + 1); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("platform page total error = %v", err)
	}
	if _, err := recoveryPageTotal(frontiercheckpoint.RecoveryPageBatchSize + 1); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("recovery page total error = %v", err)
	}
	for _, value := range []int{-1, frontierMutationBatchSize + 1} {
		if _, err := seedCursorAdvance(value); !errors.Is(
			err,
			frontiercheckpoint.ErrCorruptCheckpoint,
		) {
			t.Fatalf("seed cursor advance %d error = %v", value, err)
		}
	}
}

func TestDefaultFrontierCollaboratorsRemainInert(t *testing.T) {
	pace := alwaysDuePace{}
	now := time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC)
	job := crawljob.CrawlJob{URL: "https://example.org/"}
	if due := pace.DueAt(job, now); !due.Equal(now) {
		t.Fatalf("always-due pace returned %v", due)
	}
	pace.Visited(job, now)
	tally := noopRunTally{}
	tally.Commit([]byte("run"), yagocrawlcontract.CrawlRunTally{Fetched: 1})
	if snapshot := tally.Snapshot([]byte("run")); snapshot != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("noop tally snapshot = %+v", snapshot)
	}
	tally.Restore([]byte("run"), yagocrawlcontract.CrawlRunTally{Fetched: 1})
	frontier := NewFrontier(1, nil)
	if frontier.LeaseBindingChanges() == nil {
		t.Fatal("lease binding change channel is nil")
	}
}

func TestPageHostProgressUsesURLFallbackAndRetainsNewerPace(t *testing.T) {
	run := &crawlRun{pageHostProgress: make(map[string]stagedPageHostProgress)}
	job := crawljob.CrawlJob{URL: "https://example.org/page"}
	run.stagePageHostProgress(
		job,
		"example.org",
		frontiercheckpoint.HostProgress{
			Pace:         crawlpace.HostState{Generation: 5},
			PaceCapacity: 8,
		},
		[]string{"https://example.org/dropped"},
	)
	run.stagePageHostProgress(
		job,
		"example.org",
		frontiercheckpoint.HostProgress{
			Pace:         crawlpace.HostState{Generation: 4},
			PaceCapacity: 2,
		},
		nil,
	)
	progress := run.checkpointPageHostProgress(job)
	if progress == nil || progress.Progress.Pace.Generation != 5 ||
		progress.Progress.PaceCapacity != 8 || len(progress.DroppedURLs) != 1 {
		t.Fatalf("retained page host progress = %+v", progress)
	}
	run.clearPageHostProgress(job)
	if progress := run.checkpointPageHostProgress(job); progress != nil {
		t.Fatalf("cleared page host progress = %+v", progress)
	}
}
