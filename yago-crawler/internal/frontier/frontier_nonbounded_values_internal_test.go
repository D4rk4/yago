package frontier

import (
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestFrontierPageTotalsConvertRepresentableValues(t *testing.T) {
	pages, err := platformPageTotal(17)
	if err != nil || pages != 17 {
		t.Fatalf("platform page total = %d, %v", pages, err)
	}
	advance, err := seedCursorAdvance(19)
	if err != nil || advance != 19 {
		t.Fatalf("seed cursor advance = %d, %v", advance, err)
	}
}

func TestSuccessfulHostOutcomeGenerationOverflowFailsClosed(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("successful-host-overflow-identity")
	provenance := []byte("successful-host-overflow")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.org/page"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	work := internalReceive(t, frontier)
	frontier.mu.Lock()
	run := frontier.state.runs[seeded.RunID]
	run.hostFailures["example.org"] = 1
	run.hostGenerations["example.org"] = math.MaxUint64
	frontier.mu.Unlock()
	frontier.RecordHostFetchOutcome(t.Context(), work, false)
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("host generation overflow failure = %v", frontier.CheckpointFailure())
	}
}

func TestSeedRunPropagatesLazyManifestReadFailure(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("lazy-seed-read-identity")
	provenance := []byte("lazy-seed-read")
	readFailure := errors.New("load lazy seed manifest")
	snapshot := checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal)
	snapshot.RecoveryBounded = true
	snapshot.RecoveryComplete = true
	snapshot.SeedLength = 1
	checkpoint := &boundedCheckpointScript{
		scriptedCheckpoint: scriptedCheckpoint{status: frontiercheckpoint.RunActive},
		boundedSnapshot:    snapshot,
		seedPagesError:     readFailure,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	if seeded.Queued != 0 || !errors.Is(frontier.CheckpointFailure(), readFailure) {
		t.Fatalf(
			"failed lazy seed = %+v, %v",
			seeded,
			frontier.CheckpointFailure(),
		)
	}
}

func TestRunPendingExcludesSeedRecoverySentinel(t *testing.T) {
	profile := internalProfile(t)
	frontier := NewFrontier(1, nil)
	runID := uuid.New()
	frontier.mu.Lock()
	frontier.state.beginRun(runID, []byte("seed-recovery-pending"), profile, nil)
	frontier.state.runs[runID].seedRecovery = true
	frontier.mu.Unlock()
	if pending := frontier.RunPending(runID); pending != 0 {
		t.Fatalf("seed recovery pending = %d, want 0", pending)
	}
}
