package frontier

import (
	"testing"

	"github.com/google/uuid"
)

func TestBoundedSeedRefillWaitsForInitialSeedingTransition(t *testing.T) {
	crawlFrontier := NewFrontier(8, nil)
	profile := internalProfile(t)
	runID := uuid.New()
	crawlFrontier.mu.Lock()
	crawlFrontier.state.beginRun(runID, []byte("initial-seeding"), profile, func(bool) {})
	run := crawlFrontier.state.runs[runID]
	run.seedRecovery = true
	run.recoveryComplete = true
	run.seedRecoveryLength = 1
	crawlFrontier.mu.Unlock()

	if _, selected := crawlFrontier.selectBoundedSeedRefill(); selected {
		t.Fatal("seed refill selected a run whose initial seeding transition was active")
	}

	crawlFrontier.mu.Lock()
	run.seeding = false
	crawlFrontier.mu.Unlock()
	selected, found := crawlFrontier.selectBoundedSeedRefill()
	if !found || selected.runID != runID {
		t.Fatalf("eligible seed refill = %+v, %t", selected, found)
	}
}
