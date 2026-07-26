package crawlbroker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var errAbandonedReclaimProbe = errors.New("reclaim write failed")

// Checkpoint affinity parks an expired lease for its own worker to resume, and
// adoption needs that worker's id, which lives in the crawler's frontier
// database. A crawler whose data directory is lost returns under a new id and
// can never adopt, so without a deadline the order stayed leased forever and its
// URL could never be crawled again. The lease must come back after the deadline.
func TestAbandonedCheckpointLeaseIsReclaimedAfterTheDeadline(t *testing.T) {
	set := withClock(t)
	base := time.Unix(120_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOneForSession(t, queue, "worker-gone", "worker-gone", "session-gone")

	// Expired, but well inside the deadline: affinity still holds the lease.
	set(base.Add(2 * time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep inside deadline: %v", err)
	}
	if pendingCount(t, queue) != 0 {
		t.Fatalf(
			"pending orders inside deadline = %d, want the lease still parked",
			pendingCount(t, queue),
		)
	}
	if _, found := leaseRecordFor(t, queue, leaseID); !found {
		t.Fatal("lease was released inside the deadline")
	}

	// Past the deadline the worker is treated as gone and the order is requeued.
	set(base.Add(queue.leaseAbandonAfter() + 2*time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep past deadline: %v", err)
	}
	if pendingCount(t, queue) != 1 {
		t.Fatalf("pending orders past deadline = %d, want the order back", pendingCount(t, queue))
	}
	if _, found := leaseRecordFor(t, queue, leaseID); found {
		t.Fatal("abandoned lease survived its deadline")
	}
}

// A different worker can then claim the reclaimed order, which is the whole
// point: the work becomes available to the replacement crawler.
func TestReclaimedOrderIsClaimableByAReplacementWorker(t *testing.T) {
	set := withClock(t)
	base := time.Unix(130_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseOneForSession(t, queue, "worker-gone", "worker-gone", "session-gone")
	set(base.Add(queue.leaseAbandonAfter() + 2*time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep past deadline: %v", err)
	}

	_, _, found, err := queue.leasePopForSession(t.Context(), "worker-new", "session-new")
	if err != nil || !found {
		t.Fatalf("replacement claim found=%v err=%v", found, err)
	}
}

// The deadline is derived from the lease TTL, so a deployment that lengthens the
// TTL lengthens the grace period with it rather than reclaiming early.
func TestLeaseAbandonDeadlineFollowsTheLeaseTTL(t *testing.T) {
	queue := memQueue(t)
	queue.leaseTTL = 90 * time.Second

	if got := queue.leaseAbandonAfter(); got != 90*time.Second*crawlLeaseAbandonMultiple {
		t.Fatalf("abandon deadline = %s", got)
	}
}

// The reclaim pass is disjoint from the ordinary expiry sweep, so its failures
// have to surface on their own. A lease that still names a worker session is
// skipped by the sweep and matched only by the reclaim, which makes a write
// failure here reachable exclusively through the reclaim path.
func TestAbandonedLeaseReclaimSurfacesWriteFailures(t *testing.T) {
	set := withClock(t)
	base := time.Unix(140_000, 0)
	set(base)
	fixture := scriptedQueue(t)
	fixture.queue.leaseTTL = time.Minute
	leaseOneForSession(t, fixture.queue, "abandoned", "worker-gone", "session-gone")
	set(base.Add(fixture.queue.leaseAbandonAfter() + 2*time.Minute))
	fixture.engine.deleteErrors[leaseBucket] = errAbandonedReclaimProbe

	if err := fixture.queue.sweepExpired(t.Context()); err == nil {
		t.Fatal("expected the reclaim pass to surface its write failure")
	}
}

// The broker's lease sweep runs on its own goroutine and is only asked to stop
// asynchronously, so it can still call nowFunc while a test installs, moves, or
// releases a scripted clock. Reading the clock concurrently with those changes
// must stay safe; before the clock was guarded, this pattern raced on both the
// installed function and the time it reported.
func TestScriptedClockSurvivesConcurrentReaders(t *testing.T) {
	set := withClock(t)
	base := time.Unix(150_000, 0)
	set(base)

	readers := 4
	stop := make(chan struct{})
	var running sync.WaitGroup
	running.Add(readers)
	for range readers {
		go func() {
			defer running.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = nowFunc()
				}
			}
		}()
	}
	for step := range 200 {
		set(base.Add(time.Duration(step) * time.Second))
	}
	close(stop)
	running.Wait()

	if got := nowFunc(); !got.Equal(base.Add(199 * time.Second)) {
		t.Fatalf("scripted clock = %s, want the last value set", got)
	}
}

// Releasing a scripted clock returns the package to ambient time rather than
// leaving every later reader frozen at the zero time.
func TestScriptedClockReleaseRestoresAmbientTime(t *testing.T) {
	inner := t.Run("scripted", func(t *testing.T) {
		set := withClock(t)
		set(time.Unix(160_000, 0))
		if got := nowFunc(); !got.Equal(time.Unix(160_000, 0)) {
			t.Fatalf("scripted clock = %s", got)
		}
	})
	if !inner {
		t.Fatal("scripted subtest failed")
	}
	if nowFunc().IsZero() {
		t.Fatal("releasing the scripted clock left readers frozen")
	}
}
