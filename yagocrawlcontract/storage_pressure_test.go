package yagocrawlcontract

import (
	"context"
	"errors"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoragePressurePolicySaturatesRecoveryThreshold(t *testing.T) {
	policy := StoragePressurePolicy{
		ReservedFreeBytes:       math.MaxUint64 - 1,
		RecoveryHysteresisBytes: 2,
	}
	if got := policy.RecoveryAvailableBytes(); got != math.MaxUint64 {
		t.Fatalf("recovery threshold = %d, want %d", got, uint64(math.MaxUint64))
	}
}

func TestStoragePressureGateEntersAndLeavesWithHysteresis(t *testing.T) {
	available := uint64(101)
	now := time.Unix(1, 0)
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 100, RecoveryHysteresisBytes: 20},
		func(string) (uint64, error) { return available, nil },
		func() time.Time { return now },
		time.Second,
	)
	if err := gate.CheckGrowth(); err != nil {
		t.Fatalf("initial growth check: %v", err)
	}
	available = 100
	now = now.Add(time.Second)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStoragePressure) {
		t.Fatalf("pressure entry error = %v", err)
	}
	available = 119
	now = now.Add(time.Second)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStoragePressure) {
		t.Fatalf("hysteresis error = %v", err)
	}
	available = 120
	now = now.Add(time.Second)
	if err := gate.CheckGrowth(); err != nil {
		t.Fatalf("recovery growth check: %v", err)
	}
	snapshot := gate.Snapshot()
	if snapshot.Pressured || !snapshot.MeasurementAvailable || snapshot.AvailableBytes != 120 {
		t.Fatalf("recovered snapshot = %+v", snapshot)
	}
	if snapshot.RejectedGrowthTotal != 2 {
		t.Fatalf("rejected growth = %d, want 2", snapshot.RejectedGrowthTotal)
	}
}

func TestStoragePressureGateFailsClosedAndCachesMeasurement(t *testing.T) {
	failure := errors.New("statfs failed")
	calls := 0
	now := time.Unix(2, 0)
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{},
		func(string) (uint64, error) {
			calls++

			return 0, failure
		},
		func() time.Time { return now },
		time.Second,
	)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStorageAvailabilityUnavailable) {
		t.Fatalf("measurement error = %v", err)
	}
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStorageAvailabilityUnavailable) {
		t.Fatalf("cached measurement error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("measurement calls = %d, want 1", calls)
	}
	snapshot := gate.Snapshot()
	if snapshot.MeasurementAvailable || !snapshot.Pressured ||
		snapshot.MeasurementFailuresTotal != 1 || snapshot.RejectedGrowthTotal != 2 {
		t.Fatalf("failure snapshot = %+v", snapshot)
	}
}

func TestStoragePressureGateRequiresOperationHeadroomAboveReserve(t *testing.T) {
	available := uint64(151)
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 100},
		func(string) (uint64, error) { return available, nil },
		time.Now,
		time.Second,
	)
	if err := gate.CheckGrowthWithHeadroom(51); !errors.Is(err, ErrStorageHeadroom) {
		t.Fatalf("equal boundary error = %v", err)
	}
	if err := gate.CheckGrowthWithHeadroom(50); err != nil {
		t.Fatalf("available headroom error = %v", err)
	}
	if storageHeadroomAvailable(math.MaxUint64, math.MaxUint64, 1) {
		t.Fatal("overflowing headroom threshold was admitted")
	}
}

func TestStorageMaintenanceForcesFreshAvailabilityMeasurement(t *testing.T) {
	samples := []uint64{200, 120}
	calls := 0
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 100},
		func(string) (uint64, error) {
			sample := samples[calls]
			calls++

			return sample, nil
		},
		time.Now,
		time.Hour,
	)
	if err := gate.CheckGrowthWithHeadroom(50); err != nil {
		t.Fatalf("prime optimistic sample: %v", err)
	}
	operated := false
	err := gate.RunMaintenanceWithHeadroom(
		func() (uint64, error) { return 50, nil },
		func(uint64) error {
			operated = true

			return nil
		},
	)
	if !errors.Is(err, ErrStorageHeadroom) || operated || calls != 2 {
		t.Fatalf("maintenance error=%v operated=%t measurements=%d", err, operated, calls)
	}
}

func TestStorageMaintenanceSerializesMeasurementAndOperation(t *testing.T) {
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{},
		func(string) (uint64, error) { return 1 << 30, nil },
		time.Now,
		time.Hour,
	)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- gate.RunMaintenanceWithHeadroom(
			func() (uint64, error) { return 1, nil },
			func(uint64) error {
				close(firstStarted)
				<-releaseFirst

				return nil
			},
		)
	}()
	<-firstStarted
	secondMeasured := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- gate.RunMaintenanceWithHeadroom(
			func() (uint64, error) {
				close(secondMeasured)

				return 1, nil
			},
			func(uint64) error { return nil },
		)
	}()
	select {
	case <-secondMeasured:
		t.Fatal("second maintenance measured while first operation was active")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first maintenance: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second maintenance: %v", err)
	}
}

func TestStorageMaintenancePropagatesMeasurementAndOperationFailures(t *testing.T) {
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{},
		func(string) (uint64, error) { return 1 << 30, nil },
		time.Now,
		time.Hour,
	)
	measurementFailure := errors.New("headroom measurement failed")
	operated := false
	err := gate.RunMaintenanceWithHeadroom(
		func() (uint64, error) { return 0, measurementFailure },
		func(uint64) error {
			operated = true

			return nil
		},
	)
	if !errors.Is(err, measurementFailure) || operated {
		t.Fatalf("measurement failure error=%v operated=%t", err, operated)
	}
	operationFailure := errors.New("maintenance failed")
	err = gate.RunMaintenanceWithHeadroom(
		func() (uint64, error) { return 1, nil },
		func(uint64) error { return operationFailure },
	)
	if !errors.Is(err, operationFailure) {
		t.Fatalf("operation failure error=%v", err)
	}
}

func TestStoragePressurePolicyChangeReevaluatesAndWakesWaiter(t *testing.T) {
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 100, RecoveryHysteresisBytes: 20},
		func(string) (uint64, error) { return 50, nil },
		time.Now,
		time.Hour,
	)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStoragePressure) {
		t.Fatalf("initial pressure error = %v", err)
	}
	admitted := make(chan bool, 1)
	go func() { admitted <- gate.WaitForGrowth(t.Context()) }()
	gate.SetPolicy(StoragePressurePolicy{ReservedFreeBytes: 10})
	select {
	case got := <-admitted:
		if !got {
			t.Fatal("policy change did not admit waiter")
		}
	case <-time.After(time.Second):
		t.Fatal("policy change did not wake waiter")
	}
	if got := gate.Policy(); got.ReservedFreeBytes != 10 {
		t.Fatalf("policy = %+v", got)
	}
}

func TestStoragePressureWaitStopsWithContext(t *testing.T) {
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 1},
		func(string) (uint64, error) { return 0, nil },
		time.Now,
		0,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if gate.WaitForGrowth(ctx) {
		t.Fatal("cancelled wait admitted growth")
	}
}

func TestStoragePressureWaitRefreshesUntilRecovery(t *testing.T) {
	var available atomic.Uint64
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 1},
		func(string) (uint64, error) { return available.Load(), nil },
		time.Now,
		time.Millisecond,
	)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStoragePressure) {
		t.Fatalf("initial pressure error = %v", err)
	}
	result := make(chan bool, 1)
	go func() { result <- gate.WaitForGrowth(t.Context()) }()
	available.Store(2)
	select {
	case admitted := <-result:
		if !admitted {
			t.Fatal("recovered wait rejected growth")
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not observe recovery")
	}
}

func TestStoragePressureWaitersShareCachedMeasurements(t *testing.T) {
	var calls atomic.Uint64
	var available atomic.Uint64
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{ReservedFreeBytes: 1},
		func(string) (uint64, error) {
			calls.Add(1)

			return available.Load(), nil
		},
		time.Now,
		10*time.Millisecond,
	)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStoragePressure) {
		t.Fatalf("initial pressure error = %v", err)
	}
	const waiters = 64
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	results := make(chan bool, waiters)
	for range waiters {
		go func() { results <- gate.WaitForGrowth(ctx) }()
	}
	time.Sleep(15 * time.Millisecond)
	available.Store(2)
	for range waiters {
		if !<-results {
			t.Fatal("recovered waiter rejected growth")
		}
	}
	if measured := calls.Load(); measured > 5 {
		t.Fatalf("shared waiters made %d availability measurements", measured)
	}
}

func TestStorageAvailabilityMeasurementUsesFilesystemState(t *testing.T) {
	directory := t.TempDir()
	gate := NewStoragePressureGate(directory, StoragePressurePolicy{})
	if err := gate.CheckGrowth(); err != nil {
		t.Fatalf("default gate growth check: %v", err)
	}
	available, err := measureStorageAvailability(directory)
	if err != nil {
		t.Fatalf("measure temp directory: %v", err)
	}
	if available == 0 {
		t.Fatal("temp directory reports no available storage")
	}
	if _, err := measureStorageAvailability(t.TempDir() + "/missing"); err == nil {
		t.Fatal("missing path measurement succeeded")
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("temp directory stat: %v", err)
	}
}

func TestStoragePressurePolicyChangeRetainsFailClosedMeasurement(t *testing.T) {
	gate := newStoragePressureGate(
		"data",
		StoragePressurePolicy{},
		func(string) (uint64, error) { return 0, errors.New("unavailable") },
		time.Now,
		time.Second,
	)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStorageAvailabilityUnavailable) {
		t.Fatalf("measurement error = %v", err)
	}
	gate.SetPolicy(StoragePressurePolicy{ReservedFreeBytes: 10})
	if !gate.Snapshot().Pressured {
		t.Fatal("policy change cleared fail-closed pressure")
	}
}

func TestStorageAvailableBytesValidatesAndSaturates(t *testing.T) {
	if _, err := storageAvailableBytes(1, 0); err == nil {
		t.Fatal("zero block size accepted")
	}
	if got, err := storageAvailableBytes(math.MaxUint64, 2); err != nil || got != math.MaxUint64 {
		t.Fatalf("saturated bytes = %d, %v", got, err)
	}
	if got, err := storageAvailableBytes(3, 4); err != nil || got != 12 {
		t.Fatalf("available bytes = %d, %v", got, err)
	}
}
