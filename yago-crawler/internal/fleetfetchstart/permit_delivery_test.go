package fleetfetchstart

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestAdmissionUsesPriorRoundTripForFollowingPermitBatch(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	allowances := make([]uint64, 0, 2)
	sequences := make([]uint64, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		allowances = append(allowances, request.GetPermitDeliveryAllowanceNanoseconds())
		sequences = append(sequences, request.GetSequence())
		clock.Advance(400 * time.Millisecond)
		if len(allowances) == 1 {
			return finiteFleetFetchStartDecision(request, 1, 0, 100, 100), nil
		}

		return finiteFleetFetchStartDecision(request, 3, 0, 500, 100), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	admission.waiting.Add(2)
	t.Cleanup(func() { admission.waiting.Add(-2) })
	for range 3 {
		if err := admission.Wait(t.Context()); err != nil {
			t.Fatalf("wait for delivery-tolerant admission: %v", err)
		}
	}
	if !reflect.DeepEqual(sequences, []uint64{1, 2}) ||
		!reflect.DeepEqual(allowances, []uint64{0, uint64(400 * time.Millisecond)}) {
		t.Fatalf("sequences/allowances = %v/%v", sequences, allowances)
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{
		100 * time.Millisecond,
		100 * time.Millisecond,
	}) {
		t.Fatalf("permit waits = %v", clock.waits)
	}
}

func TestAdmissionFreezesPermitDeliveryAllowanceAcrossSequenceReplay(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	gate := NewSessionGate()
	gate.Connected()
	allowances := make([]uint64, 0, 2)
	client := fleetFetchStartLeaseClient(func(
		_ context.Context,
		request *crawlrpc.FetchStartLeaseRequest,
	) (*crawlrpc.FetchStartLeaseDecision, error) {
		allowances = append(allowances, request.GetPermitDeliveryAllowanceNanoseconds())
		if len(allowances) == 1 {
			clock.Advance(300 * time.Millisecond)

			return nil, status.Error(codes.Unavailable, "committed response lost")
		}

		return unlimitedFleetFetchStartDecision(request), nil
	})
	admission := newFleetFetchStartTestAdmission(clock, gate, client, func() uint32 { return 10 })
	_, generation, _ := gate.Snapshot()
	admission.sessionGeneration = generation
	admission.permitDeliveryAllowance = 200 * time.Millisecond
	if err := admission.Wait(t.Context()); err != nil {
		t.Fatalf("replay delivery-tolerant lease: %v", err)
	}
	if !reflect.DeepEqual(allowances, []uint64{
		uint64(200 * time.Millisecond),
		uint64(200 * time.Millisecond),
	}) {
		t.Fatalf("replayed allowances = %v", allowances)
	}
}

func TestAdmissionSpacesLatePermitConsumptionWithoutCatchUp(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	admission := &Admission{now: clock.Now, wait: clock.Wait}
	admission.lease = localFetchStartLease{
		sequence:       1,
		permits:        3,
		firstOpensAt:   clock.Now(),
		firstClosesAt:  clock.Now().Add(500 * time.Millisecond),
		permitInterval: 100 * time.Millisecond,
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); !admitted ||
		err != nil {
		t.Fatalf("first permit = %t, error = %v", admitted, err)
	}
	clock.Advance(250 * time.Millisecond)
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); !admitted ||
		err != nil {
		t.Fatalf("late second permit = %t, error = %v", admitted, err)
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); admitted ||
		err != nil {
		t.Fatalf("early third permit = %t, error = %v", admitted, err)
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); !admitted ||
		err != nil {
		t.Fatalf("spaced third permit = %t, error = %v", admitted, err)
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{100 * time.Millisecond}) {
		t.Fatalf("late-consumption waits = %v", clock.waits)
	}
}

func TestAdmissionSpacesPermitConsumptionAcrossLeaseSequences(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	admission := &Admission{now: clock.Now, wait: clock.Wait}
	admission.lease = localFetchStartLease{
		sequence:       1,
		permits:        1,
		firstOpensAt:   clock.Now(),
		firstClosesAt:  clock.Now().Add(500 * time.Millisecond),
		permitInterval: 100 * time.Millisecond,
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); !admitted ||
		err != nil {
		t.Fatalf("first sequence permit = %t, error = %v", admitted, err)
	}
	admission.finishLease()
	admission.lease = localFetchStartLease{
		sequence:       2,
		permits:        1,
		firstOpensAt:   clock.Now(),
		firstClosesAt:  clock.Now().Add(500 * time.Millisecond),
		permitInterval: 100 * time.Millisecond,
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); admitted ||
		err != nil {
		t.Fatalf("early second sequence permit = %t, error = %v", admitted, err)
	}
	if admitted, err := admission.usePermit(
		t.Context(),
		make(chan struct{}),
	); !admitted ||
		err != nil {
		t.Fatalf("spaced second sequence permit = %t, error = %v", admitted, err)
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{100 * time.Millisecond}) {
		t.Fatalf("cross-sequence waits = %v", clock.waits)
	}
}

func TestPermitDeliveryBoundsAndOpening(t *testing.T) {
	base := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if got := permitDeliveryAllowanceNanoseconds(-time.Nanosecond); got != 0 {
		t.Fatalf("negative wire delivery allowance = %d", got)
	}
	if got := permitDeliveryAllowanceNanoseconds(
		2 * yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance,
	); got != uint64(yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance) {
		t.Fatalf("bounded wire delivery allowance = %d", got)
	}
	if got := measuredPermitDeliveryAllowance(base, base.Add(-time.Second)); got != 0 {
		t.Fatalf("negative delivery allowance = %s", got)
	}
	if got := measuredPermitDeliveryAllowance(
		base,
		base.Add(2*yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance),
	); got != yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance {
		t.Fatalf("bounded delivery allowance = %s", got)
	}
	if got := nextLocalPermitOpening(base, time.Time{}, time.Second); !got.Equal(base) {
		t.Fatalf("initial permit opening = %s", got)
	}
	if got := nextLocalPermitOpening(
		base.Add(time.Second),
		base,
		time.Second,
	); !got.Equal(base.Add(time.Second)) {
		t.Fatalf("already-spaced permit opening = %s", got)
	}
	if got := nextLocalPermitOpening(
		base.Add(500*time.Millisecond),
		base,
		time.Second,
	); !got.Equal(base.Add(time.Second)) {
		t.Fatalf("delayed permit opening = %s", got)
	}
}

func TestAdmissionDeliveryAllowanceResetOnSessionChange(t *testing.T) {
	admission := &Admission{
		permitDeliveryAllowance:         time.Second,
		sequencePermitDeliveryAllowance: time.Second,
	}
	admission.resetForSession(7)
	if admission.permitDeliveryAllowance != 0 ||
		admission.sequencePermitDeliveryAllowance != 0 || admission.sessionGeneration != 7 {
		t.Fatalf("reset admission = %+v", admission)
	}
}

func TestAdmissionDeliveryWaitCancellation(t *testing.T) {
	clock := newFleetFetchStartTestClock()
	admission := &Admission{now: clock.Now, wait: clock.Wait}
	admission.lease = localFetchStartLease{
		sequence:       1,
		permits:        2,
		used:           1,
		firstOpensAt:   clock.Now(),
		firstClosesAt:  clock.Now().Add(time.Second),
		permitInterval: 100 * time.Millisecond,
	}
	admission.lastPermitUsedAt = clock.Now()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	admission.wait = func(ctx context.Context, _ time.Duration, _ <-chan struct{}) error {
		return ctx.Err()
	}
	if _, err := admission.usePermit(cancelled, make(chan struct{})); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled delivery wait = %v", err)
	}
}
