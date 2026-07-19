package crawlbroker

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestFleetFetchStartEndpointLeasesAndCompletesSequences(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 2,
		ReservationHorizon:  time.Second,
		LeaseLifetime:       3 * time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	server := &exchangeServer{fetchStarts: schedule}
	first, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  2,
	})
	if err != nil {
		t.Fatalf("lease first fetch starts: %v", err)
	}
	if !first.GetGranted() || first.GetSequence() != 1 || first.GetPermits() != 2 ||
		first.GetFirstPermitOpensAfterNanoseconds() != 0 ||
		first.GetFirstPermitClosesAfterNanoseconds() != int64(250*time.Millisecond) ||
		first.GetPermitIntervalNanoseconds() != int64(250*time.Millisecond) ||
		first.GetPolicyGeneration() != 1 || first.GetUnlimited() {
		t.Fatalf("first decision = %+v", first)
	}
	firstReplay, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if err != nil || firstReplay.GetPermits() != 2 {
		t.Fatalf("first replay decision = %+v, error = %v", firstReplay, err)
	}
	completed := uint64(1)
	second, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:          "worker-a",
		WorkerSessionId:   "session-a",
		Sequence:          2,
		MaximumPermits:    1,
		CompletedSequence: &completed,
	})
	if err != nil || !second.GetGranted() || second.GetSequence() != 2 ||
		second.GetPermits() != 1 {
		t.Fatalf("second decision = %+v, error = %v", second, err)
	}
	replayed, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:          "worker-a",
		WorkerSessionId:   "session-a",
		Sequence:          2,
		MaximumPermits:    1,
		CompletedSequence: &completed,
	})
	if err != nil || replayed.GetSequence() != second.GetSequence() ||
		replayed.GetPermits() != second.GetPermits() {
		t.Fatalf("replayed decision = %+v, error = %v", replayed, err)
	}
}

func TestFleetFetchStartEndpointReturnsRetryAndDropsMissedWindows(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      4,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
		RestartQuietPeriod:  500 * time.Millisecond,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	server := &exchangeServer{fetchStarts: schedule}
	request := &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	}
	retry, err := server.LeaseFetchStarts(t.Context(), request)
	if err != nil || retry.GetGranted() ||
		retry.GetRetryAfterNanoseconds() != int64(400*time.Millisecond) ||
		retry.GetPolicyGeneration() != 1 {
		t.Fatalf("retry decision = %+v, error = %v", retry, err)
	}
	clock.Advance(400 * time.Millisecond)
	granted, err := server.LeaseFetchStarts(t.Context(), request)
	if err != nil || !granted.GetGranted() || granted.GetPermits() != 1 {
		t.Fatalf("granted decision = %+v, error = %v", granted, err)
	}
	clock.Advance(200 * time.Millisecond)
	missed, err := server.LeaseFetchStarts(t.Context(), request)
	if err != nil || !missed.GetGranted() || missed.GetPermits() != 0 {
		t.Fatalf("missed decision = %+v, error = %v", missed, err)
	}
}

func TestFleetFetchStartEndpointReturnsUnlimitedBatch(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      0,
		MaximumLeasePermits: 3,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	server := &exchangeServer{fetchStarts: schedule}
	decision, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  3,
	})
	if err != nil || !decision.GetGranted() || !decision.GetUnlimited() ||
		decision.GetPermits() != 3 {
		t.Fatalf("unlimited decision = %+v, error = %v", decision, err)
	}
	replayed, err := server.LeaseFetchStarts(t.Context(), &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	})
	if err != nil || replayed.GetPermits() != 3 {
		t.Fatalf("replayed unlimited decision = %+v, error = %v", replayed, err)
	}
}

func TestFleetFetchStartEndpointRejectsInvalidAndStaleRequests(t *testing.T) {
	server := &exchangeServer{}
	if _, err := server.LeaseFetchStarts(t.Context(), nil); status.Code(err) !=
		codes.FailedPrecondition {
		t.Fatalf("missing policy status = %v", status.Code(err))
	}
	clock := newFleetFetchStartTestClock()
	schedule := newFleetFetchStartTestSchedule(t, clock, fleetFetchStartPolicy{
		PagesPerSecond:      1,
		MaximumLeasePermits: 1,
		ReservationHorizon:  100 * time.Millisecond,
		LeaseLifetime:       time.Second,
	})
	server.fetchStarts = schedule
	if _, err := server.LeaseFetchStarts(t.Context(), nil); status.Code(err) !=
		codes.InvalidArgument {
		t.Fatalf("nil request status = %v", status.Code(err))
	}
	if _, err := server.LeaseFetchStarts(
		t.Context(),
		&crawlrpc.FetchStartLeaseRequest{},
	); status.Code(
		err,
	) != codes.InvalidArgument {
		t.Fatalf("invalid request status = %v", status.Code(err))
	}
	valid := &crawlrpc.FetchStartLeaseRequest{
		WorkerId:        "worker-a",
		WorkerSessionId: "session-a",
		Sequence:        1,
		MaximumPermits:  1,
	}
	if _, err := server.LeaseFetchStarts(t.Context(), valid); status.Code(err) !=
		codes.FailedPrecondition {
		t.Fatalf("stale request status = %v", status.Code(err))
	}
	requireFleetFetchSession(t, schedule, "worker-a", "session-a")
	completed := uint64(1)
	valid.CompletedSequence = &completed
	if _, err := server.LeaseFetchStarts(t.Context(), valid); status.Code(err) !=
		codes.FailedPrecondition {
		t.Fatalf("missing completion status = %v", status.Code(err))
	}
}

func TestFleetFetchStartStatusMapsKnownErrors(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{err: errFleetFetchRequestInvalid, code: codes.InvalidArgument},
		{err: errFleetFetchSessionStale, code: codes.FailedPrecondition},
		{err: errFleetFetchLeaseOutstanding, code: codes.FailedPrecondition},
		{err: errFleetFetchSequenceStale, code: codes.FailedPrecondition},
		{err: errFleetFetchLeaseNotFound, code: codes.FailedPrecondition},
		{err: errFleetFetchLeaseSequenceMismatch, code: codes.FailedPrecondition},
		{err: errors.New("unexpected"), code: codes.Internal},
	}
	for _, test := range tests {
		if got := status.Code(fleetFetchStartStatus(test.err)); got != test.code {
			t.Fatalf("status for %v = %v, want %v", test.err, got, test.code)
		}
	}
}
