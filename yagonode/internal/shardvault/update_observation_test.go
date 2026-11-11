package shardvault

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type updateObservationEvent struct {
	phase   vault.UpdatePhase
	attempt vault.UpdateAttempt
	span    time.Duration
}

type updateObservationRecorder struct {
	mu     sync.Mutex
	events []updateObservationEvent
}

func (r *updateObservationRecorder) ObserveVaultUpdate(
	phase vault.UpdatePhase,
	attempt vault.UpdateAttempt,
	span time.Duration,
) {
	r.mu.Lock()
	r.events = append(r.events, updateObservationEvent{
		phase:   phase,
		attempt: attempt,
		span:    span,
	})
	r.mu.Unlock()
}

func (r *updateObservationRecorder) has(phase vault.UpdatePhase, attempt vault.UpdateAttempt) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.phase == phase && event.attempt == attempt && event.span >= 0 {
			return true
		}
	}

	return false
}

func TestUpdateObservationMeasuresContendedRetryPhases(t *testing.T) {
	t.Parallel()

	engine, err := openEngine(filepath.Join(t.TempDir(), "vault"), 0)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	defer engine.Close()
	if err := engine.Provision(testBucket); err != nil {
		t.Fatalf("provision test bucket: %v", err)
	}

	recorder := &updateObservationRecorder{}
	engine.SetVaultUpdateObserver(recorder)
	callbacks := 0
	err = engine.Update(context.Background(), func(txn vault.EngineTxn) error {
		callbacks++
		if callbacks == 1 {
			return errShardContended
		}

		return txn.Bucket(testBucket).Put([]byte("key"), []byte("value"))
	})
	if err != nil {
		t.Fatalf("update after contended retry: %v", err)
	}
	if callbacks != 2 {
		t.Fatalf("callback attempts = %d, want concurrent and contended attempts", callbacks)
	}

	for _, expected := range []updateObservationEvent{
		{phase: vault.UpdatePhaseReadYield, attempt: vault.UpdateAttemptInitial},
		{phase: vault.UpdatePhaseWriterAdmission, attempt: vault.UpdateAttemptConcurrent},
		{phase: vault.UpdatePhaseTransaction, attempt: vault.UpdateAttemptConcurrent},
		{phase: vault.UpdatePhaseWriterAdmission, attempt: vault.UpdateAttemptContended},
		{phase: vault.UpdatePhaseShardTransaction, attempt: vault.UpdateAttemptContended},
		{phase: vault.UpdatePhaseCommitValidation, attempt: vault.UpdateAttemptContended},
		{phase: vault.UpdatePhaseShardCommit, attempt: vault.UpdateAttemptContended},
		{phase: vault.UpdatePhaseCommit, attempt: vault.UpdateAttemptContended},
		{phase: vault.UpdatePhaseTotal, attempt: vault.UpdateAttemptComplete},
	} {
		if !recorder.has(expected.phase, expected.attempt) {
			t.Errorf("missing observation for phase %q attempt %q", expected.phase, expected.attempt)
		}
	}
}
