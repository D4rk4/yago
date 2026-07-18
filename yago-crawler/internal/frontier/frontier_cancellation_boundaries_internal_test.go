package frontier

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestPersistentCancellationPropagatesControlMutationFailure(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("cancellation-control-failure-identity")
	provenance := []byte("cancellation-control-failure")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.org/page"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	controlFailure := errors.New("persist cancellation control")
	checkpoint.controlError = controlFailure
	if frontier.CancelControl(provenance) {
		t.Fatal("failed persistent control mutation reported cancellation success")
	}
	if !errors.Is(frontier.CheckpointFailure(), controlFailure) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestPersistentCancellationRequiresBoundedRecoveryCapability(t *testing.T) {
	frontier := NewFrontier(1, nil, WithCheckpoint(&scriptedCheckpoint{}))
	run := &crawlRun{
		boundedRecovery: true,
		provenanceValue: []byte("bounded-cancellation"),
	}
	removed, seedDone, err := frontier.persistQueuedRunCancellation(
		persistedRunCancellation{
			key:            "bounded-cancellation",
			run:            run,
			durable:        true,
			recoveryCursor: 1,
			recoveryUpper:  2,
		},
	)
	if removed != 0 || seedDone || !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("bounded cancellation = %d, %t, %v", removed, seedDone, err)
	}
}

func TestPersistentCancellationRequiresSeedManifestCapability(t *testing.T) {
	frontier := NewFrontier(1, nil, WithCheckpoint(&scriptedCheckpoint{}))
	removed, seedDone, err := frontier.persistQueuedRunCancellation(
		persistedRunCancellation{
			key:          "seed-cancellation",
			run:          &crawlRun{},
			durable:      true,
			seedRecovery: true,
		},
	)
	if removed != 0 || seedDone || !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("seed cancellation = %d, %t, %v", removed, seedDone, err)
	}
}

func TestPersistentCancellationPropagatesSeedManifestFailure(t *testing.T) {
	seedFailure := errors.New("cancel seed manifest")
	checkpoint := &boundedCheckpointScript{cancelSeedError: seedFailure}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	removed, seedDone, err := frontier.persistQueuedRunCancellation(
		persistedRunCancellation{
			key:          "seed-cancellation-failure",
			run:          &crawlRun{},
			durable:      true,
			seedRecovery: true,
		},
	)
	if removed != 0 || seedDone || !errors.Is(err, seedFailure) {
		t.Fatalf("failed seed cancellation = %d, %t, %v", removed, seedDone, err)
	}
}

func TestPersistentCancellationReturnsRecoveredRunFinish(t *testing.T) {
	frontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	runID := uuid.New()
	frontier.mu.Lock()
	frontier.state.beginRun(runID, []byte("recovered-finish"), profile, func(bool) {})
	finishes := frontier.appendPersistedCancellationFinishesLocked(
		runID,
		nil,
		1,
		false,
	)
	frontier.mu.Unlock()
	if len(finishes) != 1 || finishes[0].finish == nil {
		t.Fatalf("recovered cancellation finishes = %+v", finishes)
	}
}
