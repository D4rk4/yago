package searchindex

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
