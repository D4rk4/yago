package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
)

func TestPeerReputationDropsNewObservationsUnderStoragePressure(t *testing.T) {
	ledger := &peerReputationLedgerFixture{}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatalf("open peer reputation observer: %v", err)
	}
	defer observer.Close()
	admission := &nodeGrowthAdmission{err: errors.New("pressure")}
	observer.growthAdmission = admission
	observation := []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(1, 0).UTC(),
	}}
	observer.Observe(context.Background(), observation)
	observer.Observe(context.Background(), observation)
	ledger.lock.Lock()
	attempts := ledger.attempts
	ledger.lock.Unlock()
	if attempts != 0 || admission.calls != 2 || !observer.pressureWarning.Load() {
		t.Fatalf(
			"pressure attempts=%d checks=%d warning=%t",
			attempts,
			admission.calls,
			observer.pressureWarning.Load(),
		)
	}
	admission.err = nil
	observer.Observe(context.Background(), observation)
	if len(waitForPeerApplications(t, ledger, 1)) != 1 || observer.pressureWarning.Load() {
		t.Fatal("peer reputation did not resume after storage recovery")
	}
}
