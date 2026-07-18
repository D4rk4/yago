package yagocrawlcontract

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"syscall"
	"time"
)

const storagePressureRefreshInterval = time.Second

var (
	ErrStoragePressure                = errors.New("storage reserve reached")
	ErrStorageAvailabilityUnavailable = errors.New("storage availability measurement unavailable")
	ErrStorageHeadroom                = errors.New("insufficient storage headroom")
)

type StoragePressurePolicy struct {
	ReservedFreeBytes       uint64
	RecoveryHysteresisBytes uint64
}

func (p StoragePressurePolicy) RecoveryAvailableBytes() uint64 {
	if p.RecoveryHysteresisBytes > math.MaxUint64-p.ReservedFreeBytes {
		return math.MaxUint64
	}

	return p.ReservedFreeBytes + p.RecoveryHysteresisBytes
}

type StoragePressureSnapshot struct {
	Policy                   StoragePressurePolicy
	AvailableBytes           uint64
	MeasurementAvailable     bool
	Pressured                bool
	UpdatedAt                time.Time
	RejectedGrowthTotal      uint64
	MeasurementFailuresTotal uint64
}

type storageAvailabilityMeasurement func(string) (uint64, error)

type StoragePressureGate struct {
	path            string
	measure         storageAvailabilityMeasurement
	now             func() time.Time
	refreshInterval time.Duration
	policyChanged   chan struct{}
	maintenanceMu   sync.Mutex
	mu              sync.Mutex
	snapshot        StoragePressureSnapshot
}

func NewStoragePressureGate(path string, policy StoragePressurePolicy) *StoragePressureGate {
	return newStoragePressureGate(
		path,
		policy,
		measureStorageAvailability,
		time.Now,
		storagePressureRefreshInterval,
	)
}

func newStoragePressureGate(
	path string,
	policy StoragePressurePolicy,
	measure storageAvailabilityMeasurement,
	now func() time.Time,
	refreshInterval time.Duration,
) *StoragePressureGate {
	return &StoragePressureGate{
		path:            path,
		measure:         measure,
		now:             now,
		refreshInterval: refreshInterval,
		policyChanged:   make(chan struct{}, 1),
		snapshot: StoragePressureSnapshot{
			Policy:    policy,
			Pressured: true,
		},
	}
}

func (g *StoragePressureGate) SetPolicy(policy StoragePressurePolicy) {
	g.mu.Lock()
	g.snapshot.Policy = policy
	g.evaluatePressureLocked()
	g.mu.Unlock()
	select {
	case g.policyChanged <- struct{}{}:
	default:
	}
}

func (g *StoragePressureGate) Policy() StoragePressurePolicy {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.snapshot.Policy
}

func (g *StoragePressureGate) Snapshot() StoragePressureSnapshot {
	g.mu.Lock()
	g.refreshLocked(false)
	snapshot := g.snapshot
	g.mu.Unlock()

	return snapshot
}

func (g *StoragePressureGate) CheckGrowth() error {
	return g.CheckGrowthWithHeadroom(0)
}

func (g *StoragePressureGate) CheckGrowthWithHeadroom(requiredBytes uint64) error {
	g.mu.Lock()
	g.refreshLocked(false)
	err := g.checkGrowthLocked(requiredBytes)
	g.mu.Unlock()

	return err
}

func (g *StoragePressureGate) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	g.maintenanceMu.Lock()
	defer g.maintenanceMu.Unlock()
	requiredBytes, err := measure()
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.refreshLocked(true)
	err = g.checkGrowthLocked(requiredBytes)
	g.mu.Unlock()
	if err != nil {
		return err
	}

	return operation(requiredBytes)
}

func (g *StoragePressureGate) checkGrowthLocked(requiredBytes uint64) error {
	err := g.growthErrorLocked()
	if err == nil && !storageHeadroomAvailable(
		g.snapshot.AvailableBytes,
		g.snapshot.Policy.ReservedFreeBytes,
		requiredBytes,
	) {
		err = ErrStorageHeadroom
	}
	if err != nil {
		g.snapshot.RejectedGrowthTotal++
	}

	return err
}

func storageHeadroomAvailable(available, reserved, required uint64) bool {
	if required > math.MaxUint64-reserved {
		return false
	}

	return available > reserved+required
}

func (g *StoragePressureGate) WaitForGrowth(ctx context.Context) bool {
	g.mu.Lock()
	g.refreshLocked(false)
	if g.growthErrorLocked() == nil {
		g.mu.Unlock()

		return true
	}
	g.snapshot.RejectedGrowthTotal++
	g.mu.Unlock()

	interval := g.refreshInterval
	if interval <= 0 {
		interval = storagePressureRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-g.policyChanged:
		case <-ticker.C:
		}
		g.mu.Lock()
		g.refreshLocked(false)
		admitted := g.growthErrorLocked() == nil
		g.mu.Unlock()
		if admitted {
			return true
		}
	}
}

func (g *StoragePressureGate) refreshLocked(force bool) {
	now := g.now()
	if !force && !g.snapshot.UpdatedAt.IsZero() &&
		now.Sub(g.snapshot.UpdatedAt) < g.refreshInterval {
		return
	}
	firstMeasurement := g.snapshot.UpdatedAt.IsZero()
	available, err := g.measure(g.path)
	g.snapshot.UpdatedAt = now
	if err != nil {
		g.snapshot.AvailableBytes = 0
		g.snapshot.MeasurementAvailable = false
		g.snapshot.Pressured = true
		g.snapshot.MeasurementFailuresTotal++

		return
	}
	g.snapshot.AvailableBytes = available
	g.snapshot.MeasurementAvailable = true
	if firstMeasurement {
		g.snapshot.Pressured = false
	}
	g.evaluatePressureLocked()
}

func (g *StoragePressureGate) evaluatePressureLocked() {
	if !g.snapshot.MeasurementAvailable {
		g.snapshot.Pressured = true

		return
	}
	if g.snapshot.Pressured {
		g.snapshot.Pressured = g.snapshot.AvailableBytes < g.snapshot.Policy.RecoveryAvailableBytes()

		return
	}
	g.snapshot.Pressured = g.snapshot.AvailableBytes <= g.snapshot.Policy.ReservedFreeBytes
}

func (g *StoragePressureGate) growthErrorLocked() error {
	if !g.snapshot.MeasurementAvailable {
		return ErrStorageAvailabilityUnavailable
	}
	if g.snapshot.Pressured {
		return ErrStoragePressure
	}

	return nil
}

func measureStorageAvailability(path string) (uint64, error) {
	var state syscall.Statfs_t
	if err := syscall.Statfs(path, &state); err != nil {
		return 0, fmt.Errorf("measure storage availability: %w", err)
	}

	return storageAvailableBytes(state.Bavail, state.Bsize)
}

func storageAvailableBytes(blocks uint64, blockSize int64) (uint64, error) {
	if blockSize <= 0 {
		return 0, fmt.Errorf("measure storage availability: invalid block size")
	}
	bytesPerBlock := uint64(blockSize)
	if blocks > math.MaxUint64/bytesPerBlock {
		return math.MaxUint64, nil
	}

	return blocks * bytesPerBlock, nil
}
