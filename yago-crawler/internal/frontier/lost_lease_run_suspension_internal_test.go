package frontier

import (
	"testing"
	"time"
)

func TestLostLeaseRunSuspensionDrainsOnlyTheMatchingLease(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("lost-lease-run")
	crawlFrontier := NewFrontier(4, nil)
	finished := make(chan bool, 1)
	seeded := crawlFrontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://example.org/first",
				"https://example.org/second",
			),
			Provenance: provenance,
			LeaseID:    "lost-lease",
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	job := internalReceive(t, crawlFrontier)
	if !crawlFrontier.SuspendLostLeaseRun(job) {
		t.Fatal("matching lost lease did not suspend its run")
	}
	crawlFrontier.WaitForSettlements()
	if !crawlFrontier.WasSuspended(provenance) {
		t.Fatal("lost-lease run was not marked suspended")
	}
	if pending := crawlFrontier.RunPending(seeded.RunID); pending != 0 {
		t.Fatalf("lost-lease run pending = %d, want 0", pending)
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("lost-lease suspension changed the delivery outcome")
		}
	case <-time.After(time.Second):
		t.Fatal("lost-lease run did not settle")
	}
}

func TestLostLeaseRunSuspensionPreservesAReboundRun(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("rebound-before-suspension")
	crawlFrontier := NewFrontier(4, nil)
	finished := make(chan bool, 1)
	crawlFrontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://example.org/first",
				"https://example.org/second",
			),
			Provenance: provenance,
			LeaseID:    "previous-lease",
		},
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	stale := internalReceive(t, crawlFrontier)
	if result := crawlFrontier.RebindRunLease(
		provenance,
		"previous-lease",
		"replacement-lease",
	); result != RunLeaseRebound {
		t.Fatalf("lease rebind result = %d, want rebound", result)
	}
	if crawlFrontier.SuspendLostLeaseRun(stale) {
		t.Fatal("stale lease suspended the rebound run")
	}
	if crawlFrontier.WasSuspended(provenance) {
		t.Fatal("rebound run was marked suspended")
	}
	for range 2 {
		job := internalReceive(t, crawlFrontier)
		if job.LeaseID != "replacement-lease" {
			t.Fatalf("rebound job lease = %q", job.LeaseID)
		}
		crawlFrontier.Done(job, successfulPageOutcome())
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
