package vault

import (
	"bytes"
	"context"
	"fmt"
)

type retainedBucketMigrationPage struct {
	marker  Name
	bucket  Name
	cursor  Key
	last    Key
	entries []BucketPageEntry
}

func migrateRetainedBucketSet(ctx context.Context, migration retainedBucketMigration) error {
	for _, bucket := range migration.buckets {
		if err := migrateRetainedBucket(ctx, migration, bucket); err != nil {
			return err
		}
	}

	return nil
}

func migrateRetainedBucket(
	ctx context.Context,
	migration retainedBucketMigration,
	bucket Name,
) error {
	present, err := migration.source.bucketProvisioned(ctx, bucket)
	if err != nil {
		return fmt.Errorf("inspect retained bucket %s: %w", bucket, err)
	}
	if !present {
		return nil
	}
	cursor, err := retainedBucketCursor(ctx, migration.target, migration.marker, bucket)
	if err != nil {
		return err
	}
	for {
		page, err := readRetainedBucketMigrationPage(ctx, migration.source, bucket, cursor)
		if err != nil {
			return err
		}
		if len(page.Entries) == 0 {
			return nil
		}
		last := append(Key(nil), page.Entries[len(page.Entries)-1].Key...)
		migrationPage := retainedBucketMigrationPage{
			marker:  migration.marker,
			bucket:  bucket,
			cursor:  cursor,
			last:    last,
			entries: page.Entries,
		}
		if err := writeAdmittedRetainedBucketMigrationPage(
			func() error {
				return writeRetainedBucketMigrationPage(ctx, migration.target, migrationPage)
			},
			migration.admission,
			retainedBucketMigrationPageHeadroom(migrationPage),
		); err != nil {
			return err
		}
		if err := afterRetainedBucketMigrationPage(bucket, last); err != nil {
			return fmt.Errorf("interrupt retained bucket %s migration: %w", bucket, err)
		}
		cursor = last
		if !page.More {
			return nil
		}
	}
}

func readRetainedBucketMigrationPage(
	ctx context.Context,
	source *Vault,
	bucket Name,
	cursor Key,
) (BucketPage, error) {
	var page BucketPage
	if err := source.View(ctx, func(tx *Txn) error {
		var err error
		page, err = tx.ReadBucketPage(bucket, cursor, retainedBucketMigrationPageSize)

		return err
	}); err != nil {
		return BucketPage{}, fmt.Errorf("read retained bucket %s: %w", bucket, err)
	}

	return page, nil
}

func writeRetainedBucketMigrationPage(
	ctx context.Context,
	target *Vault,
	page retainedBucketMigrationPage,
) error {
	if err := target.Update(ctx, func(tx *Txn) error {
		return applyRetainedBucketMigrationPage(tx, page)
	}); err != nil {
		return fmt.Errorf("write retained bucket %s: %w", page.bucket, err)
	}

	return nil
}

func applyRetainedBucketMigrationPage(
	tx *Txn,
	page retainedBucketMigrationPage,
) error {
	markerBucket := tx.etx.Bucket(page.marker)
	if !bytes.Equal(markerBucket.Get(retainedMigrationCursorKey(page.bucket)), page.cursor) {
		return fmt.Errorf("retained bucket %s cursor mismatch", page.bucket)
	}
	targetBucket := tx.etx.Bucket(page.bucket)
	for _, entry := range page.entries {
		if err := putRetainedBucketMigrationEntry(targetBucket, page.bucket, entry); err != nil {
			return err
		}
	}
	if err := markerBucket.Put(retainedMigrationCursorKey(page.bucket), page.last); err != nil {
		return fmt.Errorf("store retained bucket %s cursor: %w", page.bucket, err)
	}

	return nil
}

func putRetainedBucketMigrationEntry(
	target EngineBucket,
	bucket Name,
	entry BucketPageEntry,
) error {
	existing := target.Get(entry.Key)
	if existing != nil && !bytes.Equal(existing, entry.Value) {
		return fmt.Errorf("retained bucket %s target conflict", bucket)
	}
	if existing != nil {
		return nil
	}
	if err := target.Put(entry.Key, entry.Value); err != nil {
		return fmt.Errorf("store retained bucket %s row: %w", bucket, err)
	}

	return nil
}
