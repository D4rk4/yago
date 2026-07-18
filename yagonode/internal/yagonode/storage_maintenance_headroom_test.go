package yagonode

import "testing"

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
