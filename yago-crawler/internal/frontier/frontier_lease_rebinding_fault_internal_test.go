package frontier

import (
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func selectedLeaseRun(
	t *testing.T,
) (*Frontier, uuid.UUID, *crawlRun, []byte) {
	t.Helper()
	frontier := NewFrontier(2, nil)
	profile := internalProfile(t)
	runID := uuid.New()
	provenance := []byte("selected-lease-run")
	frontier.mu.Lock()
	frontier.state.beginRun(runID, provenance, profile, nil)
	run := frontier.state.runs[runID]
	run.leaseID = "old-lease"
	frontier.mu.Unlock()

	return frontier, runID, run, provenance
}

func TestSelectedLeaseRebindRecognizesRunRemoval(t *testing.T) {
	frontier, runID, run, provenance := selectedLeaseRun(t)
	frontier.mu.Lock()
	delete(frontier.state.runs, runID)
	result := frontier.rebindSelectedRunLeaseLocked(
		provenance,
		runID,
		run,
		"old-lease",
		"new-lease",
	)
	frontier.mu.Unlock()
	if result != RunLeaseAlreadyComplete {
		t.Fatalf("removed-run rebind result = %d", result)
	}
}

func TestSelectedLeaseRebindRejectsRunReplacement(t *testing.T) {
	frontier, runID, run, provenance := selectedLeaseRun(t)
	frontier.mu.Lock()
	frontier.state.runs[runID] = &crawlRun{provenanceValue: provenance}
	result := frontier.rebindSelectedRunLeaseLocked(
		provenance,
		runID,
		run,
		"old-lease",
		"new-lease",
	)
	frontier.mu.Unlock()
	if result != RunLeaseBindingConflict {
		t.Fatalf("replaced-run rebind result = %d", result)
	}
}

func TestLeaseLookupSkipsOtherProvenance(t *testing.T) {
	frontier, runID, run, provenance := selectedLeaseRun(t)
	frontier.mu.Lock()
	frontier.state.runs[uuid.New()] = &crawlRun{
		provenanceValue: []byte("other-provenance"),
	}
	selectedID, selected, found, unique := frontier.runByProvenanceLocked(provenance)
	frontier.mu.Unlock()
	if selectedID != runID || selected != run || !found || !unique {
		t.Fatalf(
			"selected run = %s, %p, %t, %t",
			selectedID,
			selected,
			found,
			unique,
		)
	}
}

func TestSelectedLeaseRebindRejectsRogueReadyBinding(t *testing.T) {
	frontier, runID, run, provenance := selectedLeaseRun(t)
	frontier.mu.Lock()
	frontier.state.ready = []crawljob.CrawlJob{{
		RunID:   runID,
		LeaseID: "rogue-lease",
	}}
	result := frontier.rebindSelectedRunLeaseLocked(
		provenance,
		runID,
		run,
		"old-lease",
		"new-lease",
	)
	frontier.mu.Unlock()
	if result != RunLeaseBindingConflict {
		t.Fatalf("rogue-ready rebind result = %d", result)
	}
}

func TestAbandonUnknownRunOnlyReleasesHost(t *testing.T) {
	frontier := NewFrontier(1, nil, WithMaxHostConcurrency(1))
	work := crawljob.CrawlJob{URL: "https://example.org/page"}
	frontier.mu.Lock()
	frontier.inflight["example.org"] = 1
	frontier.abandonJobLocked(work, nil)
	_, retained := frontier.inflight["example.org"]
	frontier.mu.Unlock()
	if retained {
		t.Fatal("unknown-run abandonment retained the host slot")
	}
}

func TestPersistentStaleLeaseAbandonmentRequeuesWork(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("stale-abandonment-identity")
	provenance := []byte("stale-abandonment")
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
			LeaseID:       "old-lease",
		},
		profile,
		nil,
	)
	stale := internalReceive(t, frontier)
	frontier.mu.Lock()
	frontier.state.runs[seeded.RunID].leaseID = "new-lease"
	frontier.mu.Unlock()
	frontier.Done(stale, successfulPageOutcome())
	requeued := internalReceive(t, frontier)
	if requeued.URL != stale.URL || requeued.LeaseID != "new-lease" {
		t.Fatalf("requeued stale work = %+v", requeued)
	}
}
