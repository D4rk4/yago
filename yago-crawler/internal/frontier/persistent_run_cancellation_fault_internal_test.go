package frontier

import (
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type checkpointWithoutQueuedCancellation struct {
	Checkpoint
}

func TestPersistentCancellationFailsWithoutQueueMutationCapability(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("missing-cancellation-capability-identity")
	provenance := []byte("missing-cancellation-capability-provenance")
	base := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier := NewFrontier(
		1,
		nil,
		WithCheckpoint(checkpointWithoutQueuedCancellation{Checkpoint: base}),
	)
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
	if frontier.CancelControl(provenance) {
		t.Fatal("cancellation without durable queue mutation reported success")
	}
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestPersistentCancellationPropagatesQueueMutationFailure(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("failed-cancellation-identity")
	provenance := []byte("failed-cancellation-provenance")
	cancelFailure := errors.New("cancel queued pages")
	checkpoint := &scriptedCheckpoint{
		snapshot:          checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		cancelQueuedError: cancelFailure,
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
	if frontier.CancelControl(provenance) {
		t.Fatal("failed durable queue mutation reported success")
	}
	failedWithExpectedCause := errors.Is(frontier.CheckpointFailure(), cancelFailure)
	if !failedWithExpectedCause || checkpoint.cancelQueuedCalls != 1 {
		t.Fatalf(
			"checkpoint failure = %v, cancellation calls = %d",
			frontier.CheckpointFailure(),
			checkpoint.cancelQueuedCalls,
		)
	}
}

func TestPendingPersistentCancellationFailsWithoutQueueMutationCapability(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("pending-missing-cancellation-capability-identity")
	provenance := []byte("pending-missing-cancellation-capability-provenance")
	base := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier := NewFrontier(
		1,
		nil,
		WithCheckpoint(checkpointWithoutQueuedCancellation{Checkpoint: base}),
	)
	frontier.Cancel(provenance)
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
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestCancellationBookkeepingRejectsUnrepresentableRecoveryTotal(t *testing.T) {
	frontier := NewFrontier(1, nil)
	frontier.mu.Lock()
	finishes := frontier.appendPersistedCancellationFinishesLocked(
		uuid.Nil,
		nil,
		uint64(math.MaxInt)+1,
		false,
	)
	frontier.mu.Unlock()
	if len(finishes) != 0 || !errors.Is(
		frontier.CheckpointFailure(),
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("finishes = %v, failure = %v", finishes, frontier.CheckpointFailure())
	}
}

func TestMemoryCancellationSkipsCheckpointMutation(t *testing.T) {
	frontier := NewFrontier(1, nil)
	removed, seedDone, err := frontier.persistQueuedRunCancellation(
		persistedRunCancellation{},
	)
	if err != nil || removed != 0 || seedDone {
		t.Fatalf("memory cancellation = %d, %t, %v", removed, seedDone, err)
	}
	if err := frontier.persistRunCancellationControl("memory", false); err != nil {
		t.Fatalf("memory cancellation control: %v", err)
	}
}
