package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type nodeSearchRebuildAdmission struct {
	admission growthAdmission
}

func (a nodeSearchRebuildAdmission) CheckGrowth() error {
	if err := a.admission.CheckGrowth(); err != nil {
		return fmt.Errorf("check node search rebuild growth: %w", err)
	}

	return nil
}

func (a nodeSearchRebuildAdmission) CheckGrowthWithHeadroom(required uint64) error {
	return checkStorageHeadroom(a.admission, required)
}

func (a nodeSearchRebuildAdmission) RebuildStorageObservation() searchindex.BleveRebuildStorageObservation {
	source, ok := a.admission.(*yagocrawlcontract.StoragePressureGate)
	if !ok || source == nil {
		return searchindex.BleveRebuildStorageObservation{}
	}
	snapshot := source.Snapshot()

	return searchindex.BleveRebuildStorageObservation{
		AvailableBytes:       snapshot.AvailableBytes,
		ReservedBytes:        snapshot.Policy.ReservedFreeBytes,
		MeasurementAvailable: snapshot.MeasurementAvailable,
	}
}
