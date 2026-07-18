package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
)

const retainedBucketMigrationPageSize = 256

var (
	afterRetainedBucketMigrationPage = func(Name, Key) error { return nil }
	beforeRetainedBucketCompletion   = func() error { return nil }
)

type atomicUpdateEngine interface {
	AtomicUpdates() bool
}

type bucketFingerprint struct {
	digest [sha256.Size]byte
	rows   int
}

type retainedBucketMigration struct {
	source    *Vault
	target    *Vault
	marker    Name
	signature []byte
	buckets   []Name
	admission RetainedBucketMigrationAdmission
}

type RetainedBucketMigrationPlan struct {
	Source  *Vault
	Target  *Vault
	Marker  Name
	Version string
	Buckets []Name
}

var MigrateRetainedBuckets = func(
	ctx context.Context,
	source *Vault,
	target *Vault,
	marker Name,
	version string,
	buckets []Name,
) error {
	return MigrateRetainedBucketsWithAdmission(
		ctx,
		RetainedBucketMigrationPlan{
			Source:  source,
			Target:  target,
			Marker:  marker,
			Version: version,
			Buckets: buckets,
		},
		nil,
	)
}

func MigrateRetainedBucketsWithAdmission(
	ctx context.Context,
	plan RetainedBucketMigrationPlan,
	admission RetainedBucketMigrationAdmission,
) error {
	migration, err := prepareRetainedBucketMigration(
		plan.Source,
		plan.Target,
		plan.Marker,
		plan.Version,
		plan.Buckets,
	)
	if err != nil {
		return err
	}
	migration.admission = admission
	markerPresent, err := migration.target.bucketProvisioned(ctx, migration.marker)
	if err != nil {
		return fmt.Errorf("inspect retained migration marker: %w", err)
	}
	if !markerPresent {
		if err := admitRetainedBucketMigration(admission, "marker provisioning"); err != nil {
			return err
		}
		if err := provisionRetainedBuckets(migration.target, []Name{migration.marker}); err != nil {
			return fmt.Errorf("provision retained migration target: %w", err)
		}
	}
	complete, err := retainedBucketMigrationComplete(
		ctx,
		migration.target,
		migration.marker,
		migration.signature,
	)
	if err != nil || complete {
		return err
	}
	if err := admitRetainedBucketMigration(admission, "provisioning"); err != nil {
		return err
	}
	if err := provisionRetainedMigrationBuckets(migration); err != nil {
		return err
	}
	if err := migrateRetainedBucketSet(ctx, migration); err != nil {
		return err
	}
	fingerprints, err := verifyRetainedMigrationBuckets(ctx, migration)
	if err != nil {
		return err
	}

	return completeRetainedBucketMigration(ctx, migration, fingerprints)
}

func prepareRetainedBucketMigration(
	source *Vault,
	target *Vault,
	marker Name,
	version string,
	buckets []Name,
) (retainedBucketMigration, error) {
	if source == nil || target == nil || source == target || marker == "" || version == "" ||
		len(buckets) == 0 {
		return retainedBucketMigration{}, fmt.Errorf("invalid retained bucket migration")
	}
	if err := requireAtomicMigrationTarget(target); err != nil {
		return retainedBucketMigration{}, err
	}
	ordered := append([]Name(nil), buckets...)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	signature := retainedBucketMigrationSignature(version, ordered)
	return retainedBucketMigration{
		source:    source,
		target:    target,
		marker:    marker,
		signature: signature,
		buckets:   ordered,
	}, nil
}

func provisionRetainedMigrationBuckets(migration retainedBucketMigration) error {
	if err := provisionRetainedBuckets(migration.target, migration.buckets); err != nil {
		return fmt.Errorf("provision retained migration target: %w", err)
	}

	return nil
}

func requireAtomicMigrationTarget(target *Vault) error {
	lease, err := target.acquireEngineLease()
	if err != nil {
		return err
	}
	defer lease.release()
	atomic, ok := lease.engine.(atomicUpdateEngine)
	if !ok || !atomic.AtomicUpdates() {
		return fmt.Errorf("retained bucket migration target is not atomic")
	}

	return nil
}

func provisionRetainedBuckets(storage *Vault, buckets []Name) error {
	lease, err := storage.acquireEngineLease()
	if err != nil {
		return err
	}
	defer lease.release()
	for _, bucket := range buckets {
		if err := lease.engine.Provision(bucket); err != nil {
			return fmt.Errorf("provision retained bucket %s: %w", bucket, err)
		}
	}

	return nil
}

func retainedBucketMigrationComplete(
	ctx context.Context,
	target *Vault,
	marker Name,
	signature []byte,
) (bool, error) {
	complete := false
	if err := target.View(ctx, func(tx *Txn) error {
		stored := tx.etx.Bucket(marker).Get(retainedMigrationCompleteKey())
		if stored == nil {
			return nil
		}
		if !bytes.Equal(stored, signature) {
			return fmt.Errorf("retained bucket migration marker mismatch")
		}
		complete = true

		return nil
	}); err != nil {
		return false, fmt.Errorf("read retained bucket migration marker: %w", err)
	}

	return complete, nil
}

func retainedBucketCursor(
	ctx context.Context,
	target *Vault,
	marker Name,
	bucket Name,
) (Key, error) {
	var cursor Key
	if err := target.View(ctx, func(tx *Txn) error {
		cursor = append(Key(nil), tx.etx.Bucket(marker).Get(retainedMigrationCursorKey(bucket))...)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("read retained bucket %s cursor: %w", bucket, err)
	}
	if len(cursor) == 0 {
		return nil, nil
	}

	return cursor, nil
}

func retainedBucketFingerprint(
	ctx context.Context,
	storage *Vault,
	bucket Name,
) (bucketFingerprint, error) {
	digest := sha256.New()
	present, err := storage.bucketProvisioned(ctx, bucket)
	if err != nil {
		return bucketFingerprint{}, fmt.Errorf("inspect retained bucket %s: %w", bucket, err)
	}
	if !present {
		var sum [sha256.Size]byte
		copy(sum[:], digest.Sum(nil))

		return bucketFingerprint{digest: sum}, nil
	}
	rows := 0
	if err := storage.View(ctx, func(tx *Txn) error {
		return tx.etx.Bucket(bucket).Scan(nil, func(key Key, value []byte) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("fingerprint retained bucket %s context: %w", bucket, err)
			}
			writeRetainedFingerprintValue(digest, key)
			writeRetainedFingerprintValue(digest, value)
			rows++

			return true, nil
		})
	}); err != nil {
		return bucketFingerprint{}, fmt.Errorf("fingerprint retained bucket %s: %w", bucket, err)
	}
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))

	return bucketFingerprint{digest: sum, rows: rows}, nil
}

func writeRetainedFingerprintValue(
	destination interface{ Write([]byte) (int, error) },
	value []byte,
) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func retainedBucketMigrationSignature(version string, buckets []Name) []byte {
	digest := sha256.New()
	writeRetainedFingerprintValue(digest, []byte(version))
	for _, bucket := range buckets {
		writeRetainedFingerprintValue(digest, []byte(bucket))
	}

	return digest.Sum(nil)
}

func retainedMigrationCursorKey(bucket Name) Key {
	return Key("cursor:" + bucket)
}

func retainedMigrationCompleteKey() Key {
	return Key("complete")
}
