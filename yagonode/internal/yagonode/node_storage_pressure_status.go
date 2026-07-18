package yagonode

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type storagePressureStatusSource struct {
	pressure *yagocrawlcontract.StoragePressureGate
}

func newStoragePressureStatusSource(
	pressure *yagocrawlcontract.StoragePressureGate,
) adminui.StoragePressureStatusSource {
	if pressure == nil {
		return nil
	}

	return storagePressureStatusSource{pressure: pressure}
}

func (s storagePressureStatusSource) StoragePressureStatus() adminui.StoragePressureStatus {
	snapshot := s.pressure.Snapshot()

	return adminui.StoragePressureStatus{
		AvailableBytes:          snapshot.AvailableBytes,
		ReservedFreeBytes:       snapshot.Policy.ReservedFreeBytes,
		PressureHysteresisBytes: snapshot.Policy.RecoveryHysteresisBytes,
		MeasurementAvailable:    snapshot.MeasurementAvailable,
		Pressured:               snapshot.Pressured,
	}
}
