package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestStoragePressureStatusSourcePreservesSnapshotAndNil(t *testing.T) {
	if newStoragePressureStatusSource(nil) != nil {
		t.Fatal("nil pressure gate produced status source")
	}
	gate := yagocrawlcontract.NewStoragePressureGate(
		t.TempDir(),
		yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes: 12, RecoveryHysteresisBytes: 3,
		},
	)
	status := newStoragePressureStatusSource(gate).StoragePressureStatus()
	if !status.MeasurementAvailable || status.ReservedFreeBytes != 12 ||
		status.PressureHysteresisBytes != 3 || status.AvailableBytes == 0 {
		t.Fatalf("storage pressure status = %+v", status)
	}
}
