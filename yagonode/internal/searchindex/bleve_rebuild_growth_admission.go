package searchindex

import "fmt"

type BleveRebuildGrowthAdmission interface {
	CheckGrowth() error
}

func firstBleveRebuildGrowthAdmission(
	admissions []BleveRebuildGrowthAdmission,
) BleveRebuildGrowthAdmission {
	if len(admissions) == 0 {
		return nil
	}

	return admissions[0]
}

func checkBleveGrowthAdmission(
	admission BleveRebuildGrowthAdmission,
	requiredBytes uint64,
	measurementAvailable bool,
) (bool, error) {
	if admission == nil {
		return false, nil
	}
	if headroom, ok := admission.(bleveRebuildHeadroomAdmission); ok && measurementAvailable {
		if err := headroom.CheckGrowthWithHeadroom(requiredBytes); err != nil {
			return true, fmt.Errorf("check storage headroom: %w", err)
		}

		return true, nil
	}
	if err := admission.CheckGrowth(); err != nil {
		return false, fmt.Errorf("check storage growth: %w", err)
	}

	return false, nil
}
