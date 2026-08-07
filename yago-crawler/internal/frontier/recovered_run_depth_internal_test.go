package frontier

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type pendingPageDepthScript struct {
	scriptedCheckpoint
	discardedSnapshot frontiercheckpoint.Snapshot
	discarded         uint64
	discardError      error
}

func (checkpoint *pendingPageDepthScript) DiscardPendingPagesBeyondDepth(
	context.Context,
	[]byte,
	int,
) (uint64, error) {
	if checkpoint.discardError != nil {
		return 0, checkpoint.discardError
	}
	checkpoint.snapshot = checkpoint.discardedSnapshot

	return checkpoint.discarded, nil
}

func TestRecoveredRunDepthLeavesNonpersistentAndEmptyStateUnchanged(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	frontier := NewFrontier(1, nil)
	if got, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		false,
	); err != nil || got.Counters != snapshot.Counters {
		t.Fatalf("nonpersistent recovered depth = %+v, %v", got, err)
	}
	snapshot.Counters.Pending = 0
	if got, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); err != nil || got.Counters.Pending != 0 {
		t.Fatalf("empty recovered depth = %+v, %v", got, err)
	}
}

func TestRecoveredRunDepthRequiresMutationOnlyForViolatingState(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	snapshot.Outstanding = []frontiercheckpoint.Page{{Depth: profile.Profile.MaxDepth}}
	checkpoint := &scriptedCheckpoint{snapshot: snapshot}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); err != nil {
		t.Fatalf("supported-depth legacy checkpoint refused: %v", err)
	}
	snapshot.Outstanding[0].Depth++
	if _, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("unsupported recovered depth error = %v", err)
	}
}

func TestRecoveredRunDepthReportsDiscardFailure(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	discardFailure := errors.New("discard depth failed")
	checkpoint := &pendingPageDepthScript{discardError: discardFailure}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(err, discardFailure) {
		t.Fatalf("recovered depth discard error = %v", err)
	}
}

func TestPrepareRunCheckpointPropagatesRecoveredDepthFailure(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	discardFailure := errors.New("prepare depth failed")
	checkpoint := &pendingPageDepthScript{
		scriptedCheckpoint: scriptedCheckpoint{
			status:   frontiercheckpoint.RunActive,
			snapshot: snapshot,
		},
		discardError: discardFailure,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.prepareRunCheckpoint(
		context.Background(),
		seed,
		profile,
	); !errors.Is(err, discardFailure) {
		t.Fatalf("prepare recovered depth error = %v", err)
	}
}

func TestRecoveredRunDepthAvoidsReloadWhenNothingWasDiscarded(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	checkpoint := &pendingPageDepthScript{
		scriptedCheckpoint: scriptedCheckpoint{loadError: errors.New("unexpected reload")},
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	got, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	)
	if err != nil || got.Counters != snapshot.Counters {
		t.Fatalf("unchanged recovered depth = %+v, %v", got, err)
	}
}

func TestRecoveredRunDepthReloadsAndValidatesChangedState(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	discarded := snapshot
	discarded.Counters.Pending--
	discarded.BudgetDiscardedPages++
	checkpoint := &pendingPageDepthScript{
		discardedSnapshot: discarded,
		discarded:         1,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	got, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	)
	if err != nil || got.Counters.Pending != discarded.Counters.Pending {
		t.Fatalf("reloaded recovered depth = %+v, %v", got, err)
	}

	invalid := discarded
	invalid.OrderIdentity = []byte("another-depth-order")
	checkpoint.discardedSnapshot = invalid
	if _, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid reloaded depth state error = %v", err)
	}

	checkpoint.loadError = errors.New("depth reload failed")
	if _, err := frontier.enforceRecoveredRunDepth(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(err, checkpoint.loadError) {
		t.Fatalf("recovered depth reload error = %v", err)
	}
}
