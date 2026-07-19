package yagonode

import (
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestNodeSearchRebuildAdmissionReportsStoragePressureSnapshot(t *testing.T) {
	var unavailable *yagocrawlcontract.StoragePressureGate
	if admission := nodeStorageRebuildAdmission([]growthAdmission{unavailable}); admission != nil {
		t.Fatalf("typed nil storage pressure became rebuild admission: %T", admission)
	}
	gate := yagocrawlcontract.NewStoragePressureGate(
		t.TempDir(),
		yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: 4096},
	)
	admission := nodeSearchRebuildAdmission{admission: gate}
	observation := admission.RebuildStorageObservation()
	if !observation.MeasurementAvailable || observation.AvailableBytes == 0 ||
		observation.ReservedBytes != 4096 {
		t.Fatalf("rebuild storage observation = %+v", observation)
	}
	fallback := nodeSearchRebuildAdmission{admission: &nodeGrowthAdmission{}}
	if observation := fallback.RebuildStorageObservation(); observation != (searchindex.BleveRebuildStorageObservation{}) {
		t.Fatalf("non-storage observation = %+v", observation)
	}
	want := errors.New("storage blocked")
	fallback.admission = &nodeGrowthAdmission{err: want}
	if err := fallback.CheckGrowth(); !errors.Is(err, want) ||
		!strings.Contains(err.Error(), "node search rebuild") {
		t.Fatalf("wrapped rebuild admission error = %v", err)
	}
}
