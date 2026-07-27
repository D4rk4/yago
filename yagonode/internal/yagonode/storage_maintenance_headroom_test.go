package yagonode

import (
	"errors"
	"testing"
)

type immediateMaintenanceAdmission struct {
	required uint64
}

func (admission *immediateMaintenanceAdmission) CheckGrowth() error {
	return nil
}

func (admission *immediateMaintenanceAdmission) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	required, err := measure()
	if err != nil {
		return err
	}
	admission.required = required

	return operation(required)
}

func TestRunStorageMaintenanceUsesSerializedAdmission(t *testing.T) {
	admission := &immediateMaintenanceAdmission{}
	operated := uint64(0)
	outcome, err := runStorageMaintenance(
		admission,
		func() (uint64, error) { return 42, nil },
		func(required uint64) error {
			operated = required

			return nil
		},
	)
	if err != nil || !outcome.Measured || !outcome.Started || outcome.RequiredBytes != 42 ||
		admission.required != 42 || operated != 42 {
		t.Fatalf(
			"maintenance outcome=%+v required=%d operated=%d error=%v",
			outcome,
			admission.required,
			operated,
			err,
		)
	}
}

// plainGrowthAdmission answers the growth question and nothing else — the shape
// the crawl-state admission presents when it has no filesystem gate underneath
// it.
type plainGrowthAdmission struct {
	err   error
	calls int
}

func (admission *plainGrowthAdmission) CheckGrowth() error {
	admission.calls++

	return admission.err
}

// An admission that cannot measure headroom is asked the plain growth question
// instead, and the callers only observe that no work happened — none of them
// reads the refusal back. So the gate could stop carrying the reason storage
// refused and every caller would still pass, leaving a compaction, rebuild, or
// shard growth deferral that names nothing an operator can act on. Both
// directions are read back here, Started proves the operation itself never ran,
// and the measured requirement survives the refusal for the deferral report.
func TestRunStorageMaintenanceRefusesThroughAPlainGrowthAdmission(t *testing.T) {
	pressure := errors.New("filesystem reserve reached")
	admission := &plainGrowthAdmission{err: pressure}
	outcome, err := runStorageMaintenance(
		admission,
		func() (uint64, error) { return 42, nil },
		func(uint64) error {
			t.Fatal("maintenance started while storage growth was refused")

			return nil
		},
	)
	if !errors.Is(err, pressure) {
		t.Fatalf("maintenance error = %v, want the growth refusal", err)
	}
	if admission.calls != 1 {
		t.Fatalf("growth question asked %d times, want once", admission.calls)
	}
	if !outcome.Measured || outcome.Started || outcome.RequiredBytes != 42 {
		t.Fatalf("refused maintenance outcome = %+v", outcome)
	}

	admission.err = nil
	started := uint64(0)
	outcome, err = runStorageMaintenance(
		admission,
		func() (uint64, error) { return 42, nil },
		func(required uint64) error {
			started = required

			return nil
		},
	)
	if err != nil || !outcome.Started || started != 42 {
		t.Fatalf("admitted maintenance outcome = %+v started = %d, err = %v", outcome, started, err)
	}
}
