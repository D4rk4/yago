package vault

import (
	"context"
	"errors"
	"testing"
)

type stagedRetainedMigrationAdmission struct {
	allowedMaintenance int
	calls              int
	maintenanceCalls   int
	minimumRequired    uint64
	err                error
}

type sequencedRetainedMigrationAdmission struct {
	calls  int
	failAt int
	err    error
}

func (admission *sequencedRetainedMigrationAdmission) CheckGrowth() error {
	admission.calls++
	if admission.calls == admission.failAt {
		return admission.err
	}

	return nil
}

func (admission *stagedRetainedMigrationAdmission) CheckGrowth() error {
	admission.calls++
	return nil
}

func (admission *stagedRetainedMigrationAdmission) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	admission.maintenanceCalls++
	required, err := measure()
	if err != nil {
		return err
	}
	if admission.minimumRequired == 0 || required < admission.minimumRequired {
		admission.minimumRequired = required
	}
	if admission.maintenanceCalls > admission.allowedMaintenance {
		return admission.err
	}

	return operation(required)
}

func TestRetainedBucketMigrationPressurePreservesPageCursorForResume(t *testing.T) {
	source, _ := newMigrationTestVault(t)
	target, _ := newMigrationTestVault(t)
	rows := retainedBucketMigrationPageSize + 44
	putMigrationOrderRows(t, source, rows)
	pressure := errors.New("storage pressure")
	admission := &stagedRetainedMigrationAdmission{allowedMaintenance: 1, err: pressure}
	err := MigrateRetainedBucketsWithAdmission(
		context.Background(),
		RetainedBucketMigrationPlan{
			Source:  source,
			Target:  target,
			Marker:  "migration",
			Version: "1",
			Buckets: []Name{migrationTestOrderBucket},
		},
		admission,
	)
	if !errors.Is(err, pressure) || admission.calls != 2 || admission.maintenanceCalls != 2 ||
		admission.minimumRequired <= retainedBucketMigrationAllocationHeadroom {
		t.Fatalf(
			"pressure error=%v admission calls=%d maintenance calls=%d required=%d",
			err,
			admission.calls,
			admission.maintenanceCalls,
			admission.minimumRequired,
		)
	}
	cursor, err := retainedBucketCursor(
		context.Background(),
		target,
		"migration",
		migrationTestOrderBucket,
	)
	if err != nil || string(cursor) != "0255" {
		t.Fatalf("committed cursor=%q error=%v", cursor, err)
	}
	if err := MigrateRetainedBucketsWithAdmission(
		context.Background(),
		RetainedBucketMigrationPlan{
			Source:  source,
			Target:  target,
			Marker:  "migration",
			Version: "1",
			Buckets: []Name{migrationTestOrderBucket},
		},
		&stagedRetainedMigrationAdmission{allowedMaintenance: 2},
	); err != nil {
		t.Fatalf("resume migration: %v", err)
	}
	assertMigrationOrderRows(t, target, rows)
	completedAdmission := &stagedRetainedMigrationAdmission{err: pressure}
	if err := MigrateRetainedBucketsWithAdmission(
		context.Background(),
		RetainedBucketMigrationPlan{
			Source:  source,
			Target:  target,
			Marker:  "migration",
			Version: "1",
			Buckets: []Name{migrationTestOrderBucket},
		},
		completedAdmission,
	); err != nil || completedAdmission.calls != 0 {
		t.Fatalf(
			"completed migration error=%v admission calls=%d",
			err,
			completedAdmission.calls,
		)
	}
}

func TestRetainedBucketMigrationRejectsEachGrowthBoundary(t *testing.T) {
	pressure := errors.New("storage pressure")
	for _, boundary := range []struct {
		name   string
		failAt int
	}{
		{name: "marker provisioning", failAt: 1},
		{name: "bucket provisioning", failAt: 2},
		{name: "completion", failAt: 3},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			source, _ := newMigrationTestVault(t)
			target, _ := newMigrationTestVault(t)
			admission := &sequencedRetainedMigrationAdmission{
				failAt: boundary.failAt,
				err:    pressure,
			}
			err := MigrateRetainedBucketsWithAdmission(
				t.Context(),
				RetainedBucketMigrationPlan{
					Source: source, Target: target, Marker: "migration", Version: "1",
					Buckets: []Name{migrationTestOrderBucket},
				},
				admission,
			)
			if !errors.Is(err, pressure) || admission.calls != boundary.failAt {
				t.Fatalf(
					"boundary error=%v calls=%d, want %v/%d",
					err,
					admission.calls,
					pressure,
					boundary.failAt,
				)
			}
		})
	}
}

func TestRetainedBucketMigrationFallbackPageAdmission(t *testing.T) {
	pressure := errors.New("storage pressure")
	admission := &sequencedRetainedMigrationAdmission{failAt: 1, err: pressure}
	written := false
	err := writeAdmittedRetainedBucketMigrationPage(
		func() error {
			written = true

			return nil
		},
		admission,
		1,
	)
	if !errors.Is(err, pressure) || written {
		t.Fatalf("fallback admission error=%v written=%t", err, written)
	}
}

func TestRetainedBucketMigrationHeadroomSaturates(t *testing.T) {
	if got := saturatingMigrationHeadroom(^uint64(0)-1, 2); got != ^uint64(0) {
		t.Fatalf("saturated migration headroom = %d, want %d", got, ^uint64(0))
	}
	if got := saturatingMigrationHeadroom(4, 3); got != 7 {
		t.Fatalf("added migration headroom = %d, want 7", got)
	}
}
