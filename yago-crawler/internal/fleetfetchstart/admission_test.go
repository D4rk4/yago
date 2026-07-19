package fleetfetchstart

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type fleetFetchStartTestClock struct {
	mutex   sync.Mutex
	current time.Time
	waits   []time.Duration
}

func (clock *fleetFetchStartTestClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()

	return clock.current
}

func (clock *fleetFetchStartTestClock) Advance(elapsed time.Duration) {
	clock.mutex.Lock()
	clock.current = clock.current.Add(elapsed)
	clock.mutex.Unlock()
}

func (clock *fleetFetchStartTestClock) Wait(
	ctx context.Context,
	wait time.Duration,
	_ <-chan struct{},
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("test clock context: %w", err)
	}
	clock.mutex.Lock()
	clock.waits = append(clock.waits, wait)
	clock.current = clock.current.Add(wait)
	clock.mutex.Unlock()

	return nil
}

type fleetFetchStartLeaseClient func(
	context.Context,
	*crawlrpc.FetchStartLeaseRequest,
) (*crawlrpc.FetchStartLeaseDecision, error)

type fleetFetchStartLeaseRequestObservation struct {
	sequence                 uint64
	completedSequence        uint64
	completedSequencePresent bool
}

func observeFleetFetchStartLeaseRequest(
	request *crawlrpc.FetchStartLeaseRequest,
) fleetFetchStartLeaseRequestObservation {
	return fleetFetchStartLeaseRequestObservation{
		sequence:                 request.GetSequence(),
		completedSequence:        request.GetCompletedSequence(),
		completedSequencePresent: request.CompletedSequence != nil,
	}
}

func (client fleetFetchStartLeaseClient) LeaseFetchStarts(
	ctx context.Context,
	request *crawlrpc.FetchStartLeaseRequest,
	_ ...grpc.CallOption,
) (*crawlrpc.FetchStartLeaseDecision, error) {
	return client(ctx, request)
}

func TestAdmissionDiscardsUnsafeRoundTripWindowsAndReusesBatch(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	requests := make([]fleetFetchStartLeaseRequestObservation, 0, 3)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, observeFleetFetchStartLeaseRequest(request))
		switch len(requests) {
		case 1:
			clock.Advance(50 * time.Millisecond)

			return finiteFleetFetchStartDecision(request, 2, 100, 140, 100), nil
		case 2:
			return finiteFleetFetchStartDecision(request, 2, 50, 150, 100), nil
		default:
			return unlimitedFleetFetchStartDecision(request), nil
		}
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	admission.waiting.Add(1)
	t.Cleanup(func() { admission.waiting.Add(-1) })
	for range 3 {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("wait for fleet admission: %v", err)
		}
	}
	if len(requests) != 3 || requests[0].sequence != 1 ||
		requests[0].completedSequencePresent || requests[1].sequence != 2 ||
		!requests[1].completedSequencePresent || requests[1].completedSequence != 1 ||
		requests[2].sequence != 3 || !requests[2].completedSequencePresent ||
		requests[2].completedSequence != 2 {
		t.Fatalf("lease requests = %+v", requests)
	}
	if want := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
	}; !reflect.DeepEqual(
		clock.waits,
		want,
	) {
		t.Fatalf("permit waits = %v, want %v", clock.waits, want)
	}
}

func TestAdmissionRetriesServerDelayWithoutChangingSequence(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	sequences := make([]uint64, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		sequences = append(sequences, request.GetSequence())
		if len(sequences) == 1 {
			return &crawlrpc.FetchStartLeaseDecision{
				Sequence:              request.GetSequence(),
				RetryAfterNanoseconds: int64(50 * time.Millisecond),
			}, nil
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("wait after retry: %v", err)
	}
	if !reflect.DeepEqual(sequences, []uint64{1, 1}) ||
		!reflect.DeepEqual(clock.waits, []time.Duration{50 * time.Millisecond}) {
		t.Fatalf("sequences/waits = %v/%v", sequences, clock.waits)
	}
}

func TestAdmissionRetriesTransientExchangeFailure(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	calls := 0
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		calls++
		if calls == 1 {
			return nil, status.Error(codes.Unavailable, "node unavailable")
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("wait after transient failure: %v", err)
	}
	if calls != 2 || !reflect.DeepEqual(clock.waits, []time.Duration{leaseRetryWait}) {
		t.Fatalf("calls/waits = %d/%v", calls, clock.waits)
	}
}

func TestAdmissionReplaysCommittedMaximumAfterDemandDecrease(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	demand := 8
	committed := uint32(0)
	requests := make([]uint32, 0, 3)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())
		switch len(requests) {
		case 1:
			committed = request.GetMaximumPermits()
			demand = 1

			return nil, status.Error(codes.Unavailable, "committed response lost")
		case 2:
			return finiteFleetFetchStartDecision(request, committed, 0, 1000, 1), nil
		default:
			return unlimitedFleetFetchStartDecision(request), nil
		}
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PagesPerSecond: func() uint32 { return 10 },
		PermitCapacity: func() int { return 8 },
		UpstreamDemand: func() int { return demand },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("replayed committed lease: %v", err)
	}
	for range committed - 1 {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("consume replayed permit: %v", err)
		}
	}
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("fresh-demand lease: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{8, 8, 1}) {
		t.Fatalf("replay/fresh maximum requests = %v", requests)
	}
}

func TestAdmissionRetainsSequenceMaximumAfterCallerCancellation(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	demand := 8
	requests := make([]uint32, 0, 2)
	cancelled, cancel := context.WithCancel(t.Context())
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())
		if len(requests) == 1 {
			demand = 1
			cancel()

			return nil, status.Error(codes.DeadlineExceeded, "response lost")
		}

		return finiteFleetFetchStartDecision(request, 8, 0, 1000, 1), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PagesPerSecond: func() uint32 { return 10 },
		PermitCapacity: func() int { return 8 },
		UpstreamDemand: func() int { return demand },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	if err := admission.Wait(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled committed request = %v", err)
	}
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("replay after caller cancellation: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{8, 8}) {
		t.Fatalf("cancel/replay maximum requests = %v", requests)
	}
}

func TestAdmissionResetsSequenceMaximumAfterReconnect(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	demand := 8
	requests := make([]uint32, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())
		if len(requests) == 1 {
			demand = 1
			gate.Disconnected()
			gate.Connected()

			return nil, status.Error(codes.Unavailable, "session replaced")
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PagesPerSecond: func() uint32 { return 10 },
		PermitCapacity: func() int { return 8 },
		UpstreamDemand: func() int { return demand },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("lease after reconnect: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{8, 1}) || admission.sessionGeneration != 3 {
		t.Fatalf("reconnect requests/generation = %v/%d", requests, admission.sessionGeneration)
	}
}

func TestAdmissionConservativeRTTWindowBoundaries(t *testing.T) {
	requestStartedAt := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		window    time.Duration
		interval  time.Duration
		roundTrip time.Duration
		discarded bool
	}{
		{
			name:      "ten-per-second-former-fifty-millisecond-boundary",
			window:    100 * time.Millisecond,
			interval:  100 * time.Millisecond,
			roundTrip: 50 * time.Millisecond,
		},
		{
			name:      "ten-per-second-below",
			window:    100 * time.Millisecond,
			interval:  100 * time.Millisecond,
			roundTrip: 99 * time.Millisecond,
		},
		{
			name:      "ten-per-second-boundary",
			window:    100 * time.Millisecond,
			interval:  100 * time.Millisecond,
			roundTrip: 100 * time.Millisecond,
			discarded: true,
		},
		{
			name:      "slow-rate-below-cap",
			window:    250 * time.Millisecond,
			interval:  500 * time.Millisecond,
			roundTrip: 249 * time.Millisecond,
		},
		{
			name:      "slow-rate-cap-boundary",
			window:    250 * time.Millisecond,
			interval:  500 * time.Millisecond,
			roundTrip: 250 * time.Millisecond,
			discarded: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			admission := &Admission{sequence: 1}
			response := &crawlrpc.FetchStartLeaseDecision{
				Granted:                           true,
				Sequence:                          1,
				Permits:                           1,
				FirstPermitClosesAfterNanoseconds: test.window.Nanoseconds(),
				PermitIntervalNanoseconds:         test.interval.Nanoseconds(),
			}
			if err := admission.applyDecision(
				requestStartedAt,
				requestStartedAt.Add(test.roundTrip),
				1,
				response,
			); err != nil {
				t.Fatalf("apply decision: %v", err)
			}
			if discarded := admission.lease.used == admission.lease.permits; discarded != test.discarded {
				t.Fatalf(
					"discarded = %t, want %t; lease=%+v",
					discarded,
					test.discarded,
					admission.lease,
				)
			}
		})
	}
}

func TestAdmissionWaitsForConnectedSession(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	admission.wait = func(context.Context, time.Duration, <-chan struct{}) error {
		gate.Connected()

		return nil
	}
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("wait for connected session: %v", err)
	}

	gate.Disconnected()
	sentinel := errors.New("session wait stopped")
	admission.wait = func(context.Context, time.Duration, <-chan struct{}) error {
		return sentinel
	}
	if err := admission.Wait(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("stopped session wait = %v", err)
	}
}

func TestAdmissionStopsCancelledPermitWait(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		return finiteFleetFetchStartDecision(request, 1, 100, 200, 100), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	sentinel := errors.New("permit wait stopped")
	admission.wait = func(context.Context, time.Duration, <-chan struct{}) error {
		return sentinel
	}
	if err := admission.Wait(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("stopped permit wait = %v", err)
	}
}

func TestAdmissionSkipsLeaseThatExpiresBeforeUse(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	_, generation, _ := gate.Snapshot()
	admission.sessionGeneration = generation
	admission.lease = localFetchStartLease{
		sequence:       1,
		permits:        2,
		firstOpensAt:   clock.Now().Add(-time.Second),
		firstClosesAt:  clock.Now().Add(-500 * time.Millisecond),
		permitInterval: 100 * time.Millisecond,
	}
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("replace expired lease: %v", err)
	}
	if admission.sequence != 2 || admission.completedSequence == nil ||
		*admission.completedSequence != 1 {
		t.Fatalf("expired lease state = %+v", admission)
	}
}

func TestAdmissionDiscardsResponseAcrossSessionGeneration(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	calls := 0
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		calls++
		if calls == 1 {
			gate.Disconnected()
			gate.Connected()
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("wait after session replacement: %v", err)
	}
	if calls != 2 || admission.sessionGeneration != 3 || admission.sequence != 1 {
		t.Fatalf("calls/generation/sequence = %d/%d/%d", calls,
			admission.sessionGeneration, admission.sequence)
	}
}

func TestAdmissionInvalidatesScheduledPermitOnDisconnect(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	calls := 0
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		calls++
		if calls == 1 {
			return finiteFleetFetchStartDecision(request, 1, 100, 200, 100), nil
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	waits := 0
	admission.wait = func(
		ctx context.Context,
		wait time.Duration,
		_ <-chan struct{},
	) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("test reconnect context: %w", err)
		}
		waits++
		clock.Advance(wait)
		gate.Disconnected()
		gate.Connected()

		return nil
	}
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("wait after permit disconnect: %v", err)
	}
	if calls != 2 || waits != 1 {
		t.Fatalf("calls/waits = %d/%d", calls, waits)
	}
}

func TestAdmissionFailsClosedAgainstLegacyNodeAtFiniteRate(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	ctx, cancel := context.WithCancel(t.Context())
	client := fleetFetchStartLeaseClient(func(
		context.Context,
		*crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		cancel()

		return nil, status.Error(codes.Unimplemented, "legacy node")
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	if err := admission.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy finite admission = %v", err)
	}
}

func TestAdmissionAllowsLegacyNodeOnlyAtUnlimitedRate(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	calls := 0
	client := fleetFetchStartLeaseClient(func(
		context.Context,
		*crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		calls++

		return nil, status.Error(codes.Unimplemented, "legacy node")
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, nil)
	admission.waiting.Add(int64(defaultMaximumLeasePermits - 1))
	t.Cleanup(func() { admission.waiting.Add(-int64(defaultMaximumLeasePermits - 1)) })
	for range defaultMaximumLeasePermits {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("legacy unlimited admission: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("legacy unlimited calls = %d", calls)
	}
}

func TestAdmissionDefaultDemandDoesNotReserveBeyondWaitingFetches(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	requests := make([]uint32, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, nil)
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("single default demand: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{1}) {
		t.Fatalf("single-waiter default requests = %v", requests)
	}
	admission.waiting.Add(int64(defaultMaximumLeasePermits - 1))
	defer admission.waiting.Add(-int64(defaultMaximumLeasePermits - 1))
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("queued default demand: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{1, defaultMaximumLeasePermits}) {
		t.Fatalf("queued default requests = %v", requests)
	}
}

func TestAdmissionUsesLiveWorkerDemandForNextLease(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	demand := 4
	requests := make([]uint32, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PermitCapacity: func() int { return demand },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	admission.waiting.Add(16)
	defer admission.waiting.Add(-16)
	for range demand {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("initial live demand: %v", err)
		}
	}
	demand = 17
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("updated live demand: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{4, 17}) {
		t.Fatalf("live demand requests = %v", requests)
	}
}

func TestAdmissionBatchesMaximumWorkerDemand(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	calls := 0
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		calls++
		if request.GetMaximumPermits() != yagocrawlcontract.MaximumFetchWorkerConcurrency {
			t.Fatalf("maximum permit request = %d, want %d", request.GetMaximumPermits(),
				yagocrawlcontract.MaximumFetchWorkerConcurrency)
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PermitCapacity: func() int { return yagocrawlcontract.MaximumFetchWorkerConcurrency },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	admission.waiting.Add(int64(yagocrawlcontract.MaximumFetchWorkerConcurrency - 1))
	defer admission.waiting.Add(-int64(
		yagocrawlcontract.MaximumFetchWorkerConcurrency - 1,
	))
	for range yagocrawlcontract.MaximumFetchWorkerConcurrency {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("maximum demand admission: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("maximum demand lease calls = %d, want 1", calls)
	}
}

func TestAdmissionMaximumConcurrencySingleWaiterRequestsOne(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	requested := uint32(0)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requested = request.GetMaximumPermits()

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PermitCapacity: func() int { return yagocrawlcontract.MaximumFetchWorkerConcurrency },
		UpstreamDemand: func() int { return 1 },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("single maximum-concurrency waiter: %v", err)
	}
	if requested != 1 {
		t.Fatalf("single maximum-concurrency request = %d, want 1", requested)
	}
}

func TestAdmissionBoundsWorkerDemand(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	demand := 0
	requests := make([]uint32, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, request.GetMaximumPermits())

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PermitCapacity: func() int { return demand },
	})
	admission.now = clock.Now
	admission.wait = clock.Wait
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("minimum bounded demand: %v", err)
	}
	demand = 1000
	admission.waiting.Add(int64(yagocrawlcontract.MaximumFetchWorkerConcurrency - 1))
	defer admission.waiting.Add(-int64(
		yagocrawlcontract.MaximumFetchWorkerConcurrency - 1,
	))
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("maximum bounded demand: %v", err)
	}
	if !reflect.DeepEqual(requests, []uint32{
		1,
		yagocrawlcontract.MaximumFetchWorkerConcurrency,
	}) {
		t.Fatalf("bounded demand requests = %v", requests)
	}
}

func TestAdmissionCancelsBeforeSerializedOperation(t *testing.T) {
	admission := NewAdmission(AdmissionConfig{
		WorkerID: "worker-a", WorkerSessionID: "session-a", Session: NewSessionGate(),
	})
	admission.operation <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := admission.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("serialized cancellation = %v", err)
	}
	<-admission.operation
	if waiting := admission.waiting.Load(); waiting != 0 {
		t.Fatalf("waiting demand after cancellation = %d", waiting)
	}
	if permits := admission.currentMaximumPermits(); permits != 1 {
		t.Fatalf("idle maximum permits = %d, want 1", permits)
	}
}

func TestAdmissionRejectsInvalidDecisions(t *testing.T) {
	tests := []struct {
		name     string
		response *crawlrpc.FetchStartLeaseDecision
		err      error
	}{
		{name: "nil"},
		{name: "sequence", response: &crawlrpc.FetchStartLeaseDecision{Sequence: 2}},
		{name: "permits", response: &crawlrpc.FetchStartLeaseDecision{Sequence: 1, Permits: 5}},
		{name: "retry", response: &crawlrpc.FetchStartLeaseDecision{Sequence: 1}},
		{name: "interval", response: &crawlrpc.FetchStartLeaseDecision{
			Granted: true, Sequence: 1, Permits: 1,
			FirstPermitOpensAfterNanoseconds:  1,
			FirstPermitClosesAfterNanoseconds: 2,
		}},
		{name: "window", response: &crawlrpc.FetchStartLeaseDecision{
			Granted: true, Sequence: 1, Permits: 1, PermitIntervalNanoseconds: 1,
			FirstPermitOpensAfterNanoseconds:  2,
			FirstPermitClosesAfterNanoseconds: 2,
		}},
		{name: "argument", err: status.Error(codes.InvalidArgument, "bad request")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newFleetFetchStartTestClock()
			gate := NewSessionGate()
			gate.Connected()
			client := fleetFetchStartLeaseClient(func(
				context.Context,
				*crawlrpc.FetchStartLeaseRequest,
			) (*crawlrpc.FetchStartLeaseDecision, error) {
				return test.response, test.err
			})
			admission := newFleetFetchStartTestAdmission(
				clock,
				gate,
				client,
				func() uint32 { return 10 },
			)
			if err := admission.Wait(t.Context()); err == nil {
				t.Fatal("invalid decision was accepted")
			}
		})
	}
}

func TestAdmissionCompletesEmptyGrantedLease(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	requests := make([]fleetFetchStartLeaseRequestObservation, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		requests = append(requests, observeFleetFetchStartLeaseRequest(request))
		if len(requests) == 1 {
			return &crawlrpc.FetchStartLeaseDecision{Granted: true, Sequence: 1}, nil
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("empty lease recovery: %v", err)
	}
	if len(requests) != 2 || !requests[1].completedSequencePresent ||
		requests[1].completedSequence != 1 {
		t.Fatalf("empty lease requests = %+v", requests)
	}
}

func TestSessionGateSignalsStateChanges(t *testing.T) {
	gate := NewSessionGate()
	connected, generation, changed := gate.Snapshot()
	if connected || generation != 0 {
		t.Fatalf("initial gate = %t/%d", connected, generation)
	}
	gate.Disconnected()
	assertFleetFetchStartSignalQuiet(t, changed)
	gate.Connected()
	assertFleetFetchStartSignal(t, changed)
	connected, generation, changed = gate.Snapshot()
	if !connected || generation != 1 {
		t.Fatalf("connected gate = %t/%d", connected, generation)
	}
	gate.Connected()
	assertFleetFetchStartSignalQuiet(t, changed)
	gate.Disconnected()
	assertFleetFetchStartSignal(t, changed)
}

func TestWaitForFleetFetchStartBoundaries(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := waitForFleetFetchStart(cancelled, 0, make(chan struct{})); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled zero wait = %v", err)
	}
	if err := waitForFleetFetchStart(cancelled, time.Second, make(chan struct{})); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled timed wait = %v", err)
	}
	changed := make(chan struct{})
	close(changed)
	if err := waitForFleetFetchStart(t.Context(), 0, changed); err != nil {
		t.Fatalf("zero change wait: %v", err)
	}
	if err := waitForFleetFetchStart(t.Context(), time.Second, changed); err != nil {
		t.Fatalf("timed change wait: %v", err)
	}
	if err := waitForFleetFetchStart(
		t.Context(),
		time.Nanosecond,
		make(chan struct{}),
	); err != nil {
		t.Fatalf("timer wait: %v", err)
	}
}

func newFleetFetchStartTestClock() *fleetFetchStartTestClock {
	return &fleetFetchStartTestClock{
		current: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
}

func newFleetFetchStartTestAdmission(
	clock *fleetFetchStartTestClock,
	gate *SessionGate,
	client LeaseClient,
	pagesPerSecond func() uint32,
) *Admission {
	admission := NewAdmission(AdmissionConfig{
		Client: client, WorkerID: "worker-a", WorkerSessionID: "session-a", Session: gate,
		PagesPerSecond: pagesPerSecond,
	})
	admission.now = clock.Now
	admission.wait = clock.Wait

	return admission
}

func finiteFleetFetchStartDecision(
	request *crawlrpc.FetchStartLeaseRequest,
	permits uint32,
	opensAfterMilliseconds int64,
	closesAfterMilliseconds int64,
	intervalMilliseconds int64,
) *crawlrpc.FetchStartLeaseDecision {
	return &crawlrpc.FetchStartLeaseDecision{
		Granted:                           true,
		Sequence:                          request.GetSequence(),
		Permits:                           permits,
		FirstPermitOpensAfterNanoseconds:  opensAfterMilliseconds * int64(time.Millisecond),
		FirstPermitClosesAfterNanoseconds: closesAfterMilliseconds * int64(time.Millisecond),
		PermitIntervalNanoseconds:         intervalMilliseconds * int64(time.Millisecond),
	}
}

func unlimitedFleetFetchStartDecision(
	request *crawlrpc.FetchStartLeaseRequest,
) *crawlrpc.FetchStartLeaseDecision {
	return &crawlrpc.FetchStartLeaseDecision{
		Granted: true, Sequence: request.GetSequence(),
		Permits: request.GetMaximumPermits(), Unlimited: true,
	}
}

func assertFleetFetchStartSignal(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("session change was not signaled")
	}
}

func assertFleetFetchStartSignalQuiet(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	select {
	case <-changed:
		t.Fatal("unchanged session was signaled")
	case <-time.After(10 * time.Millisecond):
	}
}
