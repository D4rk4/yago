package vault

import (
	"context"
	"fmt"
)

func verifyRetainedMigrationBuckets(
	ctx context.Context,
	migration retainedBucketMigration,
) (map[Name]bucketFingerprint, error) {
	fingerprints := make(map[Name]bucketFingerprint, len(migration.buckets))
	for _, bucket := range migration.buckets {
		sourceFingerprint, err := retainedBucketFingerprint(ctx, migration.source, bucket)
		if err != nil {
			return nil, err
		}
		targetFingerprint, err := retainedBucketFingerprint(ctx, migration.target, bucket)
		if err != nil {
			return nil, err
		}
		if sourceFingerprint != targetFingerprint {
			return nil, fmt.Errorf("retained bucket %s verification mismatch", bucket)
		}
		fingerprints[bucket] = targetFingerprint
	}

	return fingerprints, nil
}

func completeRetainedBucketMigration(
	ctx context.Context,
	migration retainedBucketMigration,
	fingerprints map[Name]bucketFingerprint,
) error {
	if err := admitRetainedBucketMigration(migration.admission, "completion"); err != nil {
		return err
	}
	if err := beforeRetainedBucketCompletion(); err != nil {
		return fmt.Errorf("interrupt retained bucket migration completion: %w", err)
	}
	if err := migration.target.Update(ctx, func(tx *Txn) error {
		return writeRetainedBucketMigrationCompletion(tx, migration, fingerprints)
	}); err != nil {
		return fmt.Errorf("complete retained bucket migration: %w", err)
	}

	return nil
}

func writeRetainedBucketMigrationCompletion(
	tx *Txn,
	migration retainedBucketMigration,
	fingerprints map[Name]bucketFingerprint,
) error {
	for _, bucket := range migration.buckets {
		if err := putLength(
			tx.etx.Bucket(lengthBucket),
			Key(bucket),
			fingerprints[bucket].rows,
		); err != nil {
			return fmt.Errorf("store retained bucket %s length: %w", bucket, err)
		}
	}
	if err := tx.etx.Bucket(migration.marker).Put(
		retainedMigrationCompleteKey(),
		migration.signature,
	); err != nil {
		return fmt.Errorf("store retained migration completion: %w", err)
	}

	return nil
}
