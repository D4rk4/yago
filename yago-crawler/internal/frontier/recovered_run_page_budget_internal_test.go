package frontier

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pendingPageBudgetScript struct {
	scriptedCheckpoint
	trimmedSnapshot frontiercheckpoint.Snapshot
	trimError       error
}

func (checkpoint *pendingPageBudgetScript) TrimPendingPages(
	context.Context,
	[]byte,
	uint64,
) (uint64, error) {
	if checkpoint.trimError != nil {
		return 0, checkpoint.trimError
	}
	checkpoint.snapshot = checkpoint.trimmedSnapshot

	return 1, nil
}

func recoveredPageBudgetScenario(
	t *testing.T,
) (CrawlRunSeed, crawladmission.AdmissionProfile, frontiercheckpoint.Snapshot) {
	t.Helper()
	profile := internalProfile(t)
	profile.Profile.MaxPagesPerHost = 2
	seed := CrawlRunSeed{
		Provenance:    []byte("recovered-page-budget"),
		OrderIdentity: []byte("recovered-page-budget-order"),
		Priority:      yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	}
	snapshot := checkpointSnapshot(seed.OrderIdentity, seed.Priority)
	snapshot.Counters = frontiercheckpoint.Counters{Pages: 4, Pending: 4}

	return seed, profile, snapshot
}

func TestRecoveredRunPageBudgetRejectsInvalidAccounting(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	snapshot.Counters.Pages = 3
	frontier := NewFrontier(1, nil)
	if _, err := frontier.enforceRecoveredRunPageBudget(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("invalid recovered page budget error = %v", err)
	}
}

func TestRecoveredRunPageBudgetRequiresPersistentMutationSupport(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	checkpoint := &scriptedCheckpoint{snapshot: snapshot}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunPageBudget(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("unsupported recovered page budget error = %v", err)
	}
}

func TestRecoveredRunPageBudgetReportsTrimFailure(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	trimFailure := errors.New("trim failed")
	checkpoint := &pendingPageBudgetScript{trimError: trimFailure}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunPageBudget(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(
		err,
		trimFailure,
	) {
		t.Fatalf("recovered page budget trim error = %v", err)
	}
}

func TestRecoveredRunPageBudgetReportsReloadFailure(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	reloadFailure := errors.New("reload failed")
	checkpoint := &pendingPageBudgetScript{
		scriptedCheckpoint: scriptedCheckpoint{loadError: reloadFailure},
		trimmedSnapshot:    snapshot,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunPageBudget(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(
		err,
		reloadFailure,
	) {
		t.Fatalf("recovered page budget reload error = %v", err)
	}
}

func TestRecoveredRunPageBudgetValidatesReloadedIdentity(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	trimmed := snapshot
	trimmed.OrderIdentity = []byte("another-order")
	checkpoint := &pendingPageBudgetScript{trimmedSnapshot: trimmed}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.enforceRecoveredRunPageBudget(
		context.Background(),
		seed,
		profile,
		snapshot,
		true,
	); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("reloaded identity error = %v", err)
	}
}

func TestReloadRecoveredRunSnapshotSupportsBothCheckpointReaders(t *testing.T) {
	t.Run("bounded success", func(t *testing.T) {
		want := frontiercheckpoint.Snapshot{BudgetDiscardedPages: 7}
		checkpoint := &boundedCheckpointScript{boundedSnapshot: want}
		frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
		got, err := frontier.reloadRecoveredRunSnapshot(context.Background(), []byte("bounded"))
		if err != nil || got.BudgetDiscardedPages != want.BudgetDiscardedPages {
			t.Fatalf("bounded reload = %+v, %v", got, err)
		}
	})
	t.Run("bounded failure", func(t *testing.T) {
		failure := errors.New("bounded reload failed")
		checkpoint := &boundedCheckpointScript{boundedSnapshotError: failure}
		frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
		if _, err := frontier.reloadRecoveredRunSnapshot(
			context.Background(),
			[]byte("bounded-failure"),
		); !errors.Is(
			err,
			failure,
		) {
			t.Fatalf("bounded reload error = %v", err)
		}
	})
	t.Run("legacy success", func(t *testing.T) {
		want := frontiercheckpoint.Snapshot{BudgetDiscardedPages: 5}
		checkpoint := &scriptedCheckpoint{snapshot: want}
		frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
		got, err := frontier.reloadRecoveredRunSnapshot(context.Background(), []byte("legacy"))
		if err != nil || got.BudgetDiscardedPages != want.BudgetDiscardedPages {
			t.Fatalf("legacy reload = %+v, %v", got, err)
		}
	})
	t.Run("legacy failure", func(t *testing.T) {
		failure := errors.New("legacy reload failed")
		checkpoint := &scriptedCheckpoint{loadError: failure}
		frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
		if _, err := frontier.reloadRecoveredRunSnapshot(
			context.Background(),
			[]byte("legacy-failure"),
		); !errors.Is(
			err,
			failure,
		) {
			t.Fatalf("legacy reload error = %v", err)
		}
	})
}

func TestPrepareRunCheckpointPropagatesRecoveredBudgetFailure(t *testing.T) {
	seed, profile, snapshot := recoveredPageBudgetScenario(t)
	checkpoint := &scriptedCheckpoint{
		status:   frontiercheckpoint.RunActive,
		snapshot: snapshot,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.prepareRunCheckpoint(
		context.Background(),
		seed,
		profile,
	); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("prepare recovered page budget error = %v", err)
	}
}
