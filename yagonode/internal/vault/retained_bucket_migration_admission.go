package vault

import "fmt"

type RetainedBucketMigrationAdmission interface {
	CheckGrowth() error
}

func admitRetainedBucketMigration(
	admission RetainedBucketMigrationAdmission,
	operation string,
) error {
	if admission == nil {
		return nil
	}
	if err := admission.CheckGrowth(); err != nil {
		return fmt.Errorf("admit retained bucket migration %s: %w", operation, err)
	}

	return nil
}
