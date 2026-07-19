package crawlbroker

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fleetFetchStartTestClock struct {
	current time.Time
}

func (clock *fleetFetchStartTestClock) Now() time.Time {
	return clock.current
}

func (clock *fleetFetchStartTestClock) Advance(elapsed time.Duration) {
	clock.current = clock.current.Add(elapsed)
}

func TestFleetFetchStartScheduleReservesStrictSlotsWithoutReclaim(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 3,
		ReservationHorizon:  time.Second,
		LeaseLifetime:       3 * time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")

	first := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  3,
	})
	if first.Permits != 3 || !first.FirstPermitAt.Equal(clock.Now()) ||
		first.PermitInterval != 250*time.Millisecond || first.PolicyGeneration != 1 ||
		first.Unlimited {
		t.Fatalf("first lease = %+v", first)
	}
	if snapshot := schedule.Snapshot(); snapshot.OutstandingLeaseTotal != 1 {
		t.Fatalf("outstanding leases = %d", snapshot.OutstandingLeaseTotal)
	}
	replayed := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if !reflect.DeepEqual(replayed, first) {
		t.Fatalf("replayed lease = %+v, want %+v", replayed, first)
	}
	_, err := schedule.Lease(fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        2,
		MaximumPermits:  1,
	})
	if !errors.Is(err, errFleetFetchLeaseOutstanding) {
		t.Fatalf("overlapping lease error = %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("complete first lease: %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("repeat completion: %v", err)
	}
	second := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        2,
		MaximumPermits:  2,
	})
	wantSecondStart := clock.Now().Add(750 * time.Millisecond)
	if second.Permits != 2 || !second.FirstPermitAt.Equal(wantSecondStart) {
		t.Fatalf("second lease = %+v, want start %s and two permits", second, wantSecondStart)
	}
	_, err = schedule.Lease(fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if !errors.Is(err, errFleetFetchSequenceStale) {
		t.Fatalf("stale sequence error = %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("replayed completion: %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 3); !errors.Is(
		err,
		errFleetFetchLeaseSequenceMismatch,
	) {
		t.Fatalf("future completion error = %v", err)
	}
}

func TestFleetFetchStartScheduleRoundsIntervalsTowardLowerThroughput(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      3,
		MaximumLeasePermits: 2,
		ReservationHorizon:  400 * time.Millisecond,
		LeaseLifetime:       2 * time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	lease := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  2,
	})
	wantInterval := 333_333_334 * time.Nanosecond
	if lease.PermitInterval != wantInterval || lease.PermitStartWindow != wantInterval ||
		lease.PermitInterval*3 < time.Second {
		t.Fatalf("strict non-divisor interval lease = %+v", lease)
	}
}

func TestFleetFetchStartScheduleUsesLargestBoundedNonoverlappingWindow(t *testing.T) {
	tests := []struct {
		pagesPerSecond uint32
		interval       time.Duration
		window         time.Duration
	}{
		{pagesPerSecond: 10, interval: 100 * time.Millisecond, window: 100 * time.Millisecond},
		{pagesPerSecond: 4, interval: 250 * time.Millisecond, window: 250 * time.Millisecond},
		{pagesPerSecond: 2, interval: 500 * time.Millisecond, window: 250 * time.Millisecond},
	}
	for _, test := range tests {
		clock := newFleetFetchStartTestClock()
		schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
			PagesPerSecond:      test.pagesPerSecond,
			MaximumLeasePermits: 1,
			ReservationHorizon:  250 * time.Millisecond,
			LeaseLifetime:       time.Second,
		})
		requireFleetFetchSession(t, schedule, "worker-a", "session-a")
		lease := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
			WorkerID:        "worker-a",
			WorkerSessionID: "session-a",
			Sequence:        1,
			MaximumPermits:  1,
		})
		if lease.PermitInterval != test.interval || lease.PermitStartWindow != test.window ||
			lease.PermitStartWindow > lease.PermitInterval {
			t.Fatalf("rate %d lease = %+v", test.pagesPerSecond, lease)
		}
	}
}

func TestFleetFetchStartScheduleRotatesWaitingSessions(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
		RestartQuietPeriod:  500 * time.Millisecond,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	requireFleetFetchSession(t, schedule, "worker-b", "session-b")

	aRequest := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	}
	bRequest := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-b",
		WorkerSessionID: "session-b",
		Sequence:        1,
		MaximumPermits:  1,
	}
	aWaiting := requireFleetFetchRetry(t, schedule, aRequest)
	bWaiting := requireFleetFetchRetry(t, schedule, bRequest)
	if !aWaiting.Equal(clock.Now().Add(400*time.Millisecond)) || !bWaiting.Equal(aWaiting) {
		t.Fatalf("initial retries = %s/%s", aWaiting, bWaiting)
	}
	if repeated := requireFleetFetchRetry(t, schedule, aRequest); !repeated.Equal(aWaiting) {
		t.Fatalf("repeated pending retry = %s, want %s", repeated, aWaiting)
	}

	clock.Advance(400 * time.Millisecond)
	aFirst := requireFleetFetchLease(t, schedule, aRequest)
	if !aFirst.FirstPermitAt.Equal(clock.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("worker A first slot = %s", aFirst.FirstPermitAt)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("complete worker A: %v", err)
	}
	aSecondRequest := aRequest
	aSecondRequest.Sequence = 2
	aSecondWaiting := requireFleetFetchRetry(t, schedule, aSecondRequest)
	if !aSecondWaiting.Equal(clock.Now().Add(250 * time.Millisecond)) {
		t.Fatalf("worker A second retry = %s", aSecondWaiting)
	}

	clock.Advance(250 * time.Millisecond)
	if retry := requireFleetFetchRetry(t, schedule, aSecondRequest); !retry.Equal(
		clock.Now().Add(250 * time.Millisecond),
	) {
		t.Fatalf("worker A retry after worker B rotation = %s", retry)
	}
	bFirst := requireFleetFetchLease(t, schedule, bRequest)
	if !bFirst.FirstPermitAt.Equal(clock.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("worker B slot = %s", bFirst.FirstPermitAt)
	}
	if err := schedule.CompleteLease("worker-b", "session-b", 1); err != nil {
		t.Fatalf("complete worker B: %v", err)
	}

	clock.Advance(250 * time.Millisecond)
	aSecond := requireFleetFetchLease(t, schedule, aSecondRequest)
	if !aSecond.FirstPermitAt.Equal(clock.Now().Add(100*time.Millisecond)) ||
		!bFirst.FirstPermitAt.Before(aSecond.FirstPermitAt) {
		t.Fatalf("rotation slots B/A2 = %s/%s", bFirst.FirstPermitAt, aSecond.FirstPermitAt)
	}
}

func TestFleetFetchStartScheduleAppliesRestartQuietPeriod(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      10,
		MaximumLeasePermits: 4,
		ReservationHorizon:  250 * time.Millisecond,
		LeaseLifetime:       time.Second,
		RestartQuietPeriod:  time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  4,
	}
	retryAt := requireFleetFetchRetry(t, schedule, request)
	if !retryAt.Equal(clock.Now().Add(750 * time.Millisecond)) {
		t.Fatalf("restart retry = %s", retryAt)
	}
	conflicting := request
	conflicting.Sequence = 2
	if _, err := schedule.Lease(conflicting); !errors.Is(err, errFleetFetchLeaseOutstanding) {
		t.Fatalf("conflicting pending sequence error = %v", err)
	}
	clock.Advance(749 * time.Millisecond)
	if earlyRetry := requireFleetFetchRetry(t, schedule, request); !earlyRetry.Equal(retryAt) {
		t.Fatalf("early retry = %s, want %s", earlyRetry, retryAt)
	}
	clock.Advance(time.Millisecond)
	lease := requireFleetFetchLease(t, schedule, request)
	if !lease.FirstPermitAt.Equal(clock.Now().Add(250 * time.Millisecond)) {
		t.Fatalf("first post-restart slot = %s", lease.FirstPermitAt)
	}
}

func TestFleetFetchStartScheduleRejectsDelayedPermitReplayAcrossSessions(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	requireFleetFetchSession(t, schedule, "worker-b", "session-b")
	aRequest := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	}
	bRequest := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-b",
		WorkerSessionID: "session-b",
		Sequence:        1,
		MaximumPermits:  1,
	}
	aDecision, err := schedule.Lease(aRequest)
	if err != nil || !aDecision.Granted {
		t.Fatalf("worker A decision = %+v, error = %v", aDecision, err)
	}
	requireFleetFetchRetry(t, schedule, bRequest)
	clock.Advance(150 * time.Millisecond)
	bDecision, err := schedule.Lease(bRequest)
	if err != nil || !bDecision.Granted {
		t.Fatalf("worker B decision = %+v, error = %v", bDecision, err)
	}
	aWindow, found := aDecision.Lease.PermitWindow(0)
	if !found {
		t.Fatal("worker A permit window was not found")
	}
	bWindow, found := bDecision.Lease.PermitWindow(0)
	if !found {
		t.Fatal("worker B permit window was not found")
	}
	if aWindow.ClosesAt.After(bWindow.OpensAt) {
		t.Fatalf("permit windows overlap: A=%+v B=%+v", aWindow, bWindow)
	}

	clock.Advance(100 * time.Millisecond)
	aReplay, err := schedule.Lease(aRequest)
	if err != nil || !aReplay.Granted {
		t.Fatalf("worker A replay = %+v, error = %v", aReplay, err)
	}
	aRelative, found := aReplay.RelativePermitWindow(0)
	if found || aRelative != (relativeFleetFetchStartPermitWindow{}) {
		t.Fatalf("delayed worker A window = %+v, found=%t", aRelative, found)
	}
	bReplay, err := schedule.Lease(bRequest)
	if err != nil || !bReplay.Granted {
		t.Fatalf("worker B replay = %+v, error = %v", bReplay, err)
	}
	bRelative, found := bReplay.RelativePermitWindow(0)
	if !found || bRelative.OpensAfter != 0 || bRelative.ClosesAfter != 100*time.Millisecond {
		t.Fatalf("current worker B window = %+v, found=%t", bRelative, found)
	}

	requestStartedAt := time.Unix(0, 0)
	responseReceivedAt := requestStartedAt.Add(100 * time.Millisecond)
	delayedRelative := requireRelativeFleetFetchStartPermitWindow(t, aDecision)
	localNotBefore := responseReceivedAt.Add(delayedRelative.OpensAfter)
	localDeadline := requestStartedAt.Add(delayedRelative.ClosesAfter)
	if localNotBefore.Before(localDeadline) {
		t.Fatalf(
			"delayed response retained an unsafe permit window: %s to %s",
			localNotBefore,
			localDeadline,
		)
	}
	if _, found := aDecision.Lease.PermitWindow(1); found {
		t.Fatal("out-of-range permit window was found")
	}
	if _, found := (fleetFetchStartDecision{}).RelativePermitWindow(0); found {
		t.Fatal("empty decision returned a permit window")
	}
}

func TestFleetFetchStartScheduleChangesFiniteAndUnlimitedPolicies(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      2,
		MaximumLeasePermits: 2,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")

	first := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  2,
	})
	if first.Permits != 1 || first.PermitInterval != 500*time.Millisecond {
		t.Fatalf("finite lease = %+v", first)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("complete finite lease: %v", err)
	}
	if err := schedule.SetPagesPerSecond(0); err != nil {
		t.Fatalf("enable unlimited policy: %v", err)
	}
	unlimited := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        2,
		MaximumPermits:  2,
	})
	if !unlimited.Unlimited || unlimited.Permits != 2 || unlimited.PolicyGeneration != 2 ||
		!unlimited.ExpiresAt.Equal(clock.Now().Add(time.Second)) {
		t.Fatalf("unlimited lease = %+v", unlimited)
	}
	if err := schedule.SetPagesPerSecond(0); err != nil {
		t.Fatalf("repeat unlimited policy: %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 2); err != nil {
		t.Fatalf("complete unlimited lease: %v", err)
	}
	if err := schedule.SetPagesPerSecond(5); err != nil {
		t.Fatalf("restore finite policy: %v", err)
	}
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        3,
		MaximumPermits:  2,
	}
	if retry := requireFleetFetchRetry(t, schedule, request); !retry.Equal(
		clock.Now().Add(400 * time.Millisecond),
	) {
		t.Fatalf("finite transition retry = %s", retry)
	}
	clock.Advance(400 * time.Millisecond)
	restored := requireFleetFetchLease(t, schedule, request)
	if restored.Unlimited || restored.PolicyGeneration != 3 ||
		!restored.FirstPermitAt.Equal(clock.Now().Add(100*time.Millisecond)) ||
		restored.PermitInterval != 200*time.Millisecond {
		t.Fatalf("restored finite lease = %+v", restored)
	}
	if err := schedule.SetPagesPerSecond(
		yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
	); !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func TestFleetFetchStartScheduleSpacesRateReductionAfterOldReservation(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      10,
		MaximumLeasePermits: 3,
		ReservationHorizon:  300 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	oldLease := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  3,
	})
	oldLastWindow, found := oldLease.PermitWindow(2)
	if !found || oldLastWindow.ClosesAt.After(
		clock.Now().Add(300*time.Millisecond+oldLease.PermitStartWindow),
	) {
		t.Fatalf("old reservation bound = %+v, found=%t", oldLastWindow, found)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("complete old lease: %v", err)
	}
	if err := schedule.SetPagesPerSecond(2); err != nil {
		t.Fatalf("reduce fleet rate: %v", err)
	}
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        2,
		MaximumPermits:  1,
	}
	if retry := requireFleetFetchRetry(t, schedule, request); !retry.Equal(
		clock.Now().Add(400 * time.Millisecond),
	) {
		t.Fatalf("reduced-rate retry = %s", retry)
	}
	clock.Advance(400 * time.Millisecond)
	newLease := requireFleetFetchLease(t, schedule, request)
	oldLastPermitAt := oldLease.FirstPermitAt.Add(2 * oldLease.PermitInterval)
	if newLease.PolicyGeneration != 2 ||
		newLease.FirstPermitAt.Sub(oldLastPermitAt) < 500*time.Millisecond {
		t.Fatalf("old/new leases = %+v/%+v", oldLease, newLease)
	}
}

func TestFleetFetchStartScheduleQuietsUnlimitedToFiniteTransition(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      0,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	unlimited := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if !unlimited.Unlimited {
		t.Fatalf("unlimited lease = %+v", unlimited)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); err != nil {
		t.Fatalf("complete unlimited lease: %v", err)
	}
	if err := schedule.SetPagesPerSecond(4); err != nil {
		t.Fatalf("enable finite policy: %v", err)
	}
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        2,
		MaximumPermits:  1,
	}
	if retry := requireFleetFetchRetry(t, schedule, request); !retry.Equal(
		clock.Now().Add(150 * time.Millisecond),
	) {
		t.Fatalf("finite quiet retry = %s", retry)
	}
	clock.Advance(150 * time.Millisecond)
	finite := requireFleetFetchLease(t, schedule, request)
	if !finite.FirstPermitAt.Equal(clock.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("first finite lease = %+v", finite)
	}
}

func TestFleetFetchStartScheduleDispatchesWaitingSessionOnUnlimitedTransition(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      2,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
		RestartQuietPeriod:  time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	}
	requireFleetFetchRetry(t, schedule, request)
	if err := schedule.SetPagesPerSecond(0); err != nil {
		t.Fatalf("enable unlimited policy: %v", err)
	}
	lease := requireFleetFetchLease(t, schedule, request)
	if !lease.Unlimited || lease.PolicyGeneration != 2 {
		t.Fatalf("transition lease = %+v", lease)
	}
}

func TestFleetFetchStartScheduleHandlesDisconnectAndExpiredLease(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	if err := schedule.ActivateSession("worker-a", "session-a"); err != nil {
		t.Fatalf("repeat activation: %v", err)
	}
	if err := schedule.ActivateSession("worker-a", "session-b"); !errors.Is(
		err,
		errFleetFetchSessionActive,
	) {
		t.Fatalf("conflicting activation error = %v", err)
	}

	first := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	requireDisconnectedFleetFetchSessionRejection(t, schedule)

	requireFleetFetchSession(t, schedule, "worker-a", "session-b")
	request := fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-b",
		Sequence:        1,
		MaximumPermits:  1,
	}
	if retry := requireFleetFetchRetry(t, schedule, request); !retry.Equal(
		clock.Now().Add(150 * time.Millisecond),
	) {
		t.Fatalf("replacement session retry = %s", retry)
	}
	clock.Advance(150 * time.Millisecond)
	replacement := requireFleetFetchLease(t, schedule, request)
	if !replacement.FirstPermitAt.Equal(first.FirstPermitAt.Add(250 * time.Millisecond)) {
		t.Fatalf("replacement slot = %s", replacement.FirstPermitAt)
	}

	clock.Advance(time.Second)
	snapshot := schedule.Snapshot()
	if snapshot.OutstandingLeaseTotal != 0 {
		t.Fatalf("expired outstanding leases = %d", snapshot.OutstandingLeaseTotal)
	}
	if err := schedule.SetPagesPerSecond(5); err != nil {
		t.Fatalf("change finite policy after idle period: %v", err)
	}
	next := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-b",
		Sequence:        2,
		MaximumPermits:  1,
	})
	if next.FirstPermitAt.Before(clock.Now()) {
		t.Fatalf(
			"expired lease replacement starts at %s before %s",
			next.FirstPermitAt,
			clock.Now(),
		)
	}
}

func requireDisconnectedFleetFetchSessionRejection(
	t *testing.T,
	schedule *fleetFetchStartSchedule,
) {
	t.Helper()
	if schedule.DeactivateSession("worker-a", "session-b") {
		t.Fatal("deactivated a stale session")
	}
	if !schedule.DeactivateSession("worker-a", "session-a") {
		t.Fatal("active session was not deactivated")
	}
	_, err := schedule.Lease(fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if !errors.Is(err, errFleetFetchSessionStale) {
		t.Fatalf("disconnected lease error = %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); !errors.Is(
		err,
		errFleetFetchSessionStale,
	) {
		t.Fatalf("disconnected completion error = %v", err)
	}
}

func TestFleetFetchStartScheduleRemovesDisconnectedWaitingSession(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      2,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
		RestartQuietPeriod:  time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	requireFleetFetchRetry(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID:        "worker-a",
		WorkerSessionID: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if snapshot := schedule.Snapshot(); snapshot.WaitingSessionTotal != 1 {
		t.Fatalf("waiting sessions = %d", snapshot.WaitingSessionTotal)
	}
	if !schedule.DeactivateSession("worker-a", "session-a") {
		t.Fatal("waiting session was not deactivated")
	}
	if snapshot := schedule.Snapshot(); snapshot.WaitingSessionTotal != 0 ||
		snapshot.ActiveSessionTotal != 0 {
		t.Fatalf("snapshot after disconnect = %+v", snapshot)
	}
	schedule.removeWaitingLocked(fleetFetchSessionIdentity{
		workerID:        "worker-a",
		workerSessionID: "session-a",
	})
}

func TestFleetFetchStartScheduleValidatesPolicyRequestsAndCompletion(t *testing.T) {
	valid := fleetFetchStartPolicy{
		PagesPerSecond:      2,
		MaximumLeasePermits: 2,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	}
	requireInvalidFleetFetchPolicies(t, valid)
	defaultSchedule, err := newFleetFetchStartSchedule(1)
	if err != nil {
		t.Fatalf("default schedule: %v", err)
	}
	defaultSnapshot := defaultSchedule.Snapshot()
	if defaultSnapshot.PagesPerSecond != 1 || defaultSnapshot.PolicyGeneration != 1 {
		t.Fatalf("default snapshot = %+v", defaultSnapshot)
	}
	if _, err := newFleetFetchStartSchedule(
		yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
	); !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("invalid default policy error = %v", err)
	}

	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, valid)
	requireInvalidFleetFetchRequests(t, schedule)
}

func requireInvalidFleetFetchPolicies(t *testing.T, valid fleetFetchStartPolicy) {
	t.Helper()
	invalidPolicies := []fleetFetchStartPolicy{
		{
			PagesPerSecond:      yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
			MaximumLeasePermits: 1,
			ReservationHorizon:  time.Millisecond,
			LeaseLifetime:       time.Second,
		},
		{
			MaximumLeasePermits: 0,
			ReservationHorizon:  time.Millisecond,
			LeaseLifetime:       time.Second,
		},
		{
			MaximumLeasePermits: yagocrawlcontract.MaximumFetchWorkerConcurrency + 1,
			ReservationHorizon:  time.Millisecond,
			LeaseLifetime:       time.Second,
		},
		{
			MaximumLeasePermits: 1,
			ReservationHorizon:  0,
			LeaseLifetime:       time.Second,
		},
		{
			MaximumLeasePermits: 1,
			ReservationHorizon:  time.Second,
			LeaseLifetime:       time.Second,
		},
		{
			MaximumLeasePermits: 1,
			ReservationHorizon:  time.Millisecond,
			LeaseLifetime:       time.Second,
			RestartQuietPeriod:  -time.Millisecond,
		},
	}
	for _, policy := range invalidPolicies {
		if _, err := newFleetFetchStartScheduleAt(policy, time.Now); !errors.Is(
			err,
			errFleetFetchPolicyInvalid,
		) {
			t.Fatalf("invalid policy %+v error = %v", policy, err)
		}
	}
	if _, err := newFleetFetchStartScheduleAt(valid, nil); !errors.Is(
		err,
		errFleetFetchPolicyInvalid,
	) {
		t.Fatalf("nil clock error = %v", err)
	}
}

func requireInvalidFleetFetchRequests(t *testing.T, schedule *fleetFetchStartSchedule) {
	t.Helper()
	if err := schedule.ActivateSession("", "session-a"); !errors.Is(
		err,
		errFleetFetchRequestInvalid,
	) {
		t.Fatalf("invalid activation error = %v", err)
	}
	if schedule.DeactivateSession("", "session-a") {
		t.Fatal("invalid identity was deactivated")
	}
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	invalidRequests := []fleetFetchStartLeaseRequest{
		{WorkerID: "", WorkerSessionID: "session-a", Sequence: 1, MaximumPermits: 1},
		{WorkerID: "worker-a", WorkerSessionID: "session-a", Sequence: 0, MaximumPermits: 1},
		{WorkerID: "worker-a", WorkerSessionID: "session-a", Sequence: 1, MaximumPermits: 0},
		{WorkerID: "worker-a", WorkerSessionID: "session-a", Sequence: 1, MaximumPermits: 3},
	}
	for _, request := range invalidRequests {
		if _, err := schedule.Lease(request); !errors.Is(err, errFleetFetchRequestInvalid) {
			t.Fatalf("invalid request %+v error = %v", request, err)
		}
	}
	if err := schedule.CompleteLease("", "session-a", 1); !errors.Is(
		err,
		errFleetFetchRequestInvalid,
	) {
		t.Fatalf("invalid completion identity error = %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 0); !errors.Is(
		err,
		errFleetFetchRequestInvalid,
	) {
		t.Fatalf("invalid completion sequence error = %v", err)
	}
	if err := schedule.CompleteLease("worker-a", "session-a", 1); !errors.Is(
		err,
		errFleetFetchLeaseNotFound,
	) {
		t.Fatalf("missing lease completion error = %v", err)
	}
}

func newFleetFetchStartTestClock() *fleetFetchStartTestClock {
	return &fleetFetchStartTestClock{current: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)}
}

func newFleetFetchStartTestSchedule(
	t *testing.T,
	clock *fleetFetchStartTestClock,
	policy fleetFetchStartPolicy,
) *fleetFetchStartSchedule {
	t.Helper()
	schedule, err := newFleetFetchStartScheduleAt(policy, clock.Now)
	if err != nil {
		t.Fatalf("new fleet fetch-start schedule: %v", err)
	}

	return schedule
}

func requireRelativeFleetFetchStartPermitWindow(
	t *testing.T,
	decision fleetFetchStartDecision,
) relativeFleetFetchStartPermitWindow {
	t.Helper()
	window, found := decision.RelativePermitWindow(0)
	if !found {
		t.Fatal("relative permit window was not found")
	}

	return window
}

func requireFleetFetchSession(
	t *testing.T,
	schedule *fleetFetchStartSchedule,
	workerID string,
	workerSessionID string,
) {
	t.Helper()
	if err := schedule.ActivateSession(workerID, workerSessionID); err != nil {
		t.Fatalf("activate %s/%s: %v", workerID, workerSessionID, err)
	}
}

func requireFleetFetchLease(
	t *testing.T,
	schedule *fleetFetchStartSchedule,
	request fleetFetchStartLeaseRequest,
) fleetFetchStartLease {
	t.Helper()
	decision, err := schedule.Lease(request)
	if err != nil {
		t.Fatalf("lease %+v: %v", request, err)
	}
	if !decision.Granted {
		t.Fatalf("lease %+v retry at %s", request, decision.RetryAt)
	}

	return decision.Lease
}

func requireFleetFetchRetry(
	t *testing.T,
	schedule *fleetFetchStartSchedule,
	request fleetFetchStartLeaseRequest,
) time.Time {
	t.Helper()
	decision, err := schedule.Lease(request)
	if err != nil {
		t.Fatalf("lease %+v: %v", request, err)
	}
	if decision.Granted || decision.RetryAt.IsZero() {
		t.Fatalf("lease %+v decision = %+v", request, decision)
	}

	return decision.RetryAt
}
