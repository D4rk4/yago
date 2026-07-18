package crawlbroker

import (
	"errors"
	"testing"
	"time"
)

func TestLeaseRenewalDeduplicatesInputAndCommitsLiveLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(90_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOneForSession(t, queue, "renew", "worker", testWorkerSessionID)
	set(base.Add(20 * time.Second))
	candidates, refresh, err := queue.leaseRenewalCandidates(
		t.Context(),
		"worker",
		testWorkerSessionID,
		[]string{"", leaseID, leaseID},
		base.Add(20*time.Second),
	)
	if err != nil || !refresh || len(candidates) != 1 {
		t.Fatalf("candidates=%#v refresh=%v error=%v", candidates, refresh, err)
	}
	renewed, remaining, err := queue.renewLeases(
		t.Context(),
		"worker",
		testWorkerSessionID,
		[]string{leaseID},
	)
	if err != nil || len(renewed) != 1 || renewed[0] != leaseID || remaining != time.Minute {
		t.Fatalf("renewed=%v remaining=%v error=%v", renewed, remaining, err)
	}
}

func TestLeaseRenewalSurfacesReadAndWriteFailures(t *testing.T) {
	t.Run("candidate read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseBucket]["corrupt"] = []byte("{")
		if _, _, err := fixture.queue.leaseRenewalCandidates(
			t.Context(),
			"worker",
			testWorkerSessionID,
			[]string{"corrupt"},
			time.Now(),
		); err == nil {
			t.Fatal("candidate read failure was hidden")
		}
	})

	t.Run("commit read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.buckets[leaseBucket]["corrupt"] = []byte("{")
		if _, err := fixture.queue.commitLeaseRenewals(
			t.Context(),
			"worker",
			testWorkerSessionID,
			[]leaseRenewalCandidate{{leaseID: "corrupt"}},
			time.Now(),
		); err == nil {
			t.Fatal("commit read failure was hidden")
		}
	})

	t.Run("commit write", func(t *testing.T) {
		set := withClock(t)
		base := time.Unix(91_000, 0)
		set(base)
		fixture := scriptedQueue(t)
		fixture.queue.leaseTTL = time.Minute
		leaseID := leaseOneForSession(
			t,
			fixture.queue,
			"renew-write",
			"worker",
			testWorkerSessionID,
		)
		set(base.Add(20 * time.Second))
		fixture.engine.putErrors[leaseBucket] = errors.New("write failed")
		if _, _, err := fixture.queue.renewLeases(
			t.Context(),
			"worker",
			testWorkerSessionID,
			[]string{leaseID},
		); err == nil {
			t.Fatal("renewal write failure was hidden")
		}
	})
}

func TestRenewalResponseDropsLeaseExpiredBeforeResponse(t *testing.T) {
	at := time.Unix(92_000, 0)
	renewed, remaining := renewalResponse([]leaseRenewalCandidate{{
		leaseID: "expired",
		record:  leaseRecord{ExpiresAtUnixNano: at.UnixNano()},
	}}, at, time.Minute)
	if len(renewed) != 0 || remaining != time.Minute {
		t.Fatalf("renewed=%v remaining=%v", renewed, remaining)
	}
}

func TestWorkerLeaseCatalogReportsPersistedCorruptLease(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.buckets[leaseBucket]["corrupt"] = []byte("{")
	if _, err := newDurableOrderQueue(fixture.queue.vault, time.Minute); err == nil {
		t.Fatal("corrupt active lease was hidden")
	}
}
