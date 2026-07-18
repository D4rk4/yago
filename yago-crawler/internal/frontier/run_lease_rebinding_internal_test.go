package frontier

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

func TestRunLeaseRebindFencesStaleWorkAndRewritesReadyJobs(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("run-lease-rebind")
	frontier := NewFrontier(4, nil)
	finished := make(chan bool, 1)
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://example.org/first",
				"https://example.org/second",
			),
			Provenance: provenance,
			LeaseID:    "old-lease",
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	stale := internalReceive(t, frontier)
	assertRunLeaseRebindResults(t, frontier, provenance)
	assertStaleRunLeaseWorkRejected(t, frontier, stale, seeded)
	completeReboundRun(t, frontier, finished)
	assertCompletedRunLeaseRebindResults(t, frontier, provenance)
}

func assertRunLeaseRebindResults(t *testing.T, frontier *Frontier, provenance []byte) {
	t.Helper()
	if result := frontier.RebindRunLease(
		provenance,
		"old-lease",
		"new-lease",
	); result != RunLeaseRebound {
		t.Fatalf("rebind result = %d", result)
	}
	if result := frontier.RebindRunLease(
		provenance,
		"old-lease",
		"new-lease",
	); result != RunLeaseRebound {
		t.Fatalf("idempotent rebind result = %d", result)
	}
	if result := frontier.RebindRunLease(
		provenance,
		"wrong-lease",
		"other-lease",
	); result != RunLeaseBindingConflict {
		t.Fatalf("conflicting rebind result = %d", result)
	}
}

func assertStaleRunLeaseWorkRejected(
	t *testing.T,
	frontier *Frontier,
	stale crawljob.CrawlJob,
	seeded SeededRun,
) {
	t.Helper()
	if frontier.ResolveRedirect(stale, "https://example.org/redirect") {
		t.Fatal("stale redirect mutation was accepted")
	}
	frontier.RecordHostFetchOutcome(t.Context(), stale, true)
	if duplicates := frontier.Submit(
		t.Context(),
		stale,
		crawljob.DiscoveredLinks{Followable: []string{"https://example.org/stale-link"}},
	); duplicates != 0 {
		t.Fatalf("stale link duplicates = %d", duplicates)
	}
	frontier.Done(stale, successfulPageOutcome())
	if pending := frontier.RunPending(seeded.RunID); pending != 2 {
		t.Fatalf("pending after stale completion = %d, want 2", pending)
	}
	frontier.mu.Lock()
	run := frontier.state.runs[seeded.RunID]
	invalidHostFailures := run == nil || len(run.hostFailures) != 0
	staleLinkAdmitted := false
	if run != nil {
		_, staleLinkAdmitted = run.visited["https://example.org/stale-link"]
	}
	invalidReadyLease := ""
	for _, ready := range frontier.state.ready {
		if ready.RunID == seeded.RunID && ready.LeaseID != "new-lease" {
			invalidReadyLease = ready.LeaseID
		}
	}
	frontier.mu.Unlock()
	if invalidHostFailures {
		t.Fatalf("run host failures after stale outcome = %#v", run)
	}
	if staleLinkAdmitted {
		t.Fatal("stale discovered link entered the run")
	}
	if invalidReadyLease != "" {
		t.Fatalf("ready job lease = %q", invalidReadyLease)
	}
}

func completeReboundRun(t *testing.T, frontier *Frontier, finished <-chan bool) {
	t.Helper()
	for range 2 {
		job := internalReceive(t, frontier)
		if job.LeaseID != "new-lease" {
			t.Fatalf("replacement job lease = %q", job.LeaseID)
		}
		frontier.Done(job, successfulPageOutcome())
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("rebound run did not finish successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("rebound run did not finish")
	}
}

func assertCompletedRunLeaseRebindResults(
	t *testing.T,
	frontier *Frontier,
	provenance []byte,
) {
	t.Helper()
	if result := frontier.RebindRunLease(
		provenance,
		"new-lease",
		"later-lease",
	); result != RunLeaseAlreadyComplete {
		t.Fatalf("completed-run rebind result = %d", result)
	}
	if result := frontier.RebindRunLease(
		nil,
		"new-lease",
		"later-lease",
	); result != RunLeaseBindingConflict {
		t.Fatalf("invalid rebind result = %d", result)
	}
}

func TestRunLeaseRebindRejectsAmbiguousProvenance(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("ambiguous-run-lease")
	frontier := NewFrontier(4, nil)
	for _, rawURL := range []string{"https://one.example/", "https://two.example/"} {
		frontier.SeedRunWithPriority(
			context.Background(),
			CrawlRunSeed{
				Requests:   internalRequests(profile, rawURL),
				Provenance: provenance,
				LeaseID:    "old-lease",
			},
			profile,
			nil,
		)
	}
	if result := frontier.RebindRunLease(
		provenance,
		"old-lease",
		"new-lease",
	); result != RunLeaseBindingConflict {
		t.Fatalf("ambiguous rebind result = %d", result)
	}
}
