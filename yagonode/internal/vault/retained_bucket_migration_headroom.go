package vault

import (
	"fmt"
	"math"
)

const retainedBucketMigrationAllocationHeadroom = uint64(16 << 20)

type retainedBucketMigrationMaintenance interface {
	RunMaintenanceWithHeadroom(
		func() (uint64, error),
		func(uint64) error,
	) error
}

func retainedBucketMigrationPageHeadroom(page retainedBucketMigrationPage) uint64 {
	required := retainedBucketMigrationAllocationHeadroom
	for _, entry := range page.entries {
		required = saturatingMigrationHeadroom(required, uint64(len(entry.Key)))
		required = saturatingMigrationHeadroom(required, uint64(len(entry.Value)))
	}

	return required
}

func saturatingMigrationHeadroom(current, additional uint64) uint64 {
	if additional > math.MaxUint64-current {
		return math.MaxUint64
	}

	return current + additional
}

func writeAdmittedRetainedBucketMigrationPage(
	ctxWrite func() error,
	admission RetainedBucketMigrationAdmission,
	required uint64,
) error {
	if maintenance, ok := admission.(retainedBucketMigrationMaintenance); ok {
		if err := maintenance.RunMaintenanceWithHeadroom(
			func() (uint64, error) { return required, nil },
			func(uint64) error { return ctxWrite() },
		); err != nil {
			return fmt.Errorf("write admitted retained migration page: %w", err)
		}

		return nil
	}
	if err := admitRetainedBucketMigration(admission, "page write"); err != nil {
		return err
	}

	return ctxWrite()
}
