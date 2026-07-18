package yagonode

import "fmt"

type storageHeadroomAdmission interface {
	CheckGrowthWithHeadroom(uint64) error
}

type storageMaintenanceAdmission interface {
	RunMaintenanceWithHeadroom(
		func() (uint64, error),
		func(uint64) error,
	) error
}

type storageMaintenanceOutcome struct {
	RequiredBytes uint64
	Measured      bool
	Started       bool
}

func checkStorageHeadroom(
	admission growthAdmission,
	requiredBytes uint64,
) error {
	if admission == nil {
		return nil
	}
	if headroom, ok := admission.(storageHeadroomAdmission); ok {
		if err := headroom.CheckGrowthWithHeadroom(requiredBytes); err != nil {
			return fmt.Errorf("check storage growth headroom: %w", err)
		}

		return nil
	}
	if err := admission.CheckGrowth(); err != nil {
		return fmt.Errorf("check storage growth: %w", err)
	}

	return nil
}

func runStorageMaintenance(
	admission growthAdmission,
	measure func() (uint64, error),
	operation func(uint64) error,
) (storageMaintenanceOutcome, error) {
	outcome := storageMaintenanceOutcome{}
	measured := func() (uint64, error) {
		required, err := measure()
		if err == nil {
			outcome.RequiredBytes = required
			outcome.Measured = true
		}

		return required, err
	}
	started := func(required uint64) error {
		outcome.Started = true

		return operation(required)
	}
	if maintenance, ok := admission.(storageMaintenanceAdmission); ok {
		err := maintenance.RunMaintenanceWithHeadroom(measured, started)
		if err != nil {
			return outcome, fmt.Errorf("run storage maintenance: %w", err)
		}

		return outcome, nil
	}
	required, err := measured()
	if err != nil {
		return outcome, err
	}
	if err := checkStorageHeadroom(admission, required); err != nil {
		return outcome, err
	}

	return outcome, started(required)
}
