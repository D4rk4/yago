package crawlbroker

import (
	"errors"
	"math"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestFleetFetchStartScheduleAmortizesPermitDeliveryAllowance(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      10,
		MaximumLeasePermits: 3,
		ReservationHorizon:  250 * time.Millisecond,
		LeaseLifetime:       600 * time.Millisecond,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	requireFleetFetchSession(t, schedule, "worker-b", "session-b")
	first := requireFleetFetchLease(t, schedule, fleetFetchStartLeaseRequest{
		WorkerID: "worker-a", WorkerSessionID: "session-a", Sequence: 1,
		MaximumPermits: 3, PermitDeliveryAllowance: 400 * time.Millisecond,
	})
	if first.Permits != 3 || first.PermitInterval != 100*time.Millisecond ||
		first.PermitStartWindow != 500*time.Millisecond ||
		!first.ExpiresAt.Equal(clock.Now().Add(700*time.Millisecond)) {
		t.Fatalf("delivery-tolerant lease = %+v", first)
	}
	finalWindow, found := first.PermitWindow(2)
	if !found || !finalWindow.OpensAt.Equal(clock.Now().Add(200*time.Millisecond)) ||
		!finalWindow.ClosesAt.Equal(clock.Now().Add(700*time.Millisecond)) {
		t.Fatalf("final permit window = %+v, found=%t", finalWindow, found)
	}
	secondRequest := fleetFetchStartLeaseRequest{
		WorkerID: "worker-b", WorkerSessionID: "session-b", Sequence: 1,
		MaximumPermits: 1,
	}
	if retry := requireFleetFetchRetry(t, schedule, secondRequest); !retry.Equal(
		clock.Now().Add(450 * time.Millisecond),
	) {
		t.Fatalf("next batch retry = %s", retry)
	}
	clock.Advance(450 * time.Millisecond)
	second := requireFleetFetchLease(t, schedule, secondRequest)
	if !second.FirstPermitAt.Equal(finalWindow.ClosesAt) {
		t.Fatalf("successive batches overlap: first=%+v second=%+v", finalWindow, second)
	}
}

func TestFleetFetchStartEndpointReturnsDeliveryTolerantWindow(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      10,
		MaximumLeasePermits: 3,
		ReservationHorizon:  250 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	server := &exchangeServer{fetchStarts: schedule}
	request := &crawlrpc.FetchStartLeaseRequest{
		WorkerId: "worker-a", WorkerSessionId: "session-a", Sequence: 1,
		MaximumPermits:                     3,
		PermitDeliveryAllowanceNanoseconds: 400_000_000,
	}
	decision, err := server.LeaseFetchStarts(t.Context(), request)
	if err != nil || !decision.GetGranted() || decision.GetPermits() != 3 ||
		decision.GetFirstPermitOpensAfterNanoseconds() != 0 ||
		decision.GetFirstPermitClosesAfterNanoseconds() !=
			(500*time.Millisecond).Nanoseconds() ||
		decision.GetPermitIntervalNanoseconds() != (100*time.Millisecond).Nanoseconds() {
		t.Fatalf("delivery-tolerant decision = %+v, error = %v", decision, err)
	}
	request.PermitDeliveryAllowanceNanoseconds = 0
	replayed, err := server.LeaseFetchStarts(t.Context(), request)
	if err != nil || replayed.GetPermits() != decision.GetPermits() ||
		replayed.GetFirstPermitClosesAfterNanoseconds() !=
			decision.GetFirstPermitClosesAfterNanoseconds() {
		t.Fatalf("replayed decision = %+v, error = %v", replayed, err)
	}
	request.PermitDeliveryAllowanceNanoseconds = math.MaxUint64
	if _, err := server.LeaseFetchStarts(t.Context(), request); status.Code(err) !=
		codes.InvalidArgument {
		t.Fatalf("overflowing delivery allowance error = %v", err)
	}
}

func TestFleetFetchStartScheduleRejectsInvalidDeliveryAllowance(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      10,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	for _, allowance := range []time.Duration{
		-time.Nanosecond,
		yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance + time.Nanosecond,
	} {
		_, err := schedule.Lease(fleetFetchStartLeaseRequest{
			WorkerID: "worker-a", WorkerSessionID: "session-a", Sequence: 1,
			MaximumPermits: 1, PermitDeliveryAllowance: allowance,
		})
		if !errors.Is(err, errFleetFetchRequestInvalid) {
			t.Fatalf("allowance %s error = %v", allowance, err)
		}
	}
}
