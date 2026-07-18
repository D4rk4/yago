package crawlruns

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	legacyStorageMigrationMarker  vault.Name = "crawlrunstoragemigration"
	legacyStorageMigrationVersion string     = "1"
)

func MigrateLegacyStorage(ctx context.Context, source *vault.Vault, target *vault.Vault) error {
	return MigrateLegacyStorageWithAdmission(ctx, source, target, nil)
}

func MigrateLegacyStorageWithAdmission(
	ctx context.Context,
	source *vault.Vault,
	target *vault.Vault,
	admission vault.RetainedBucketMigrationAdmission,
) error {
	if err := vault.MigrateRetainedBucketsWithAdmission(
		ctx,
		vault.RetainedBucketMigrationPlan{
			Source:  source,
			Target:  target,
			Marker:  legacyStorageMigrationMarker,
			Version: legacyStorageMigrationVersion,
			Buckets: legacyStorageVersionOneBuckets(),
		},
		admission,
	); err != nil {
		return fmt.Errorf("migrate legacy crawl run storage: %w", err)
	}

	return nil
}

func legacyStorageVersionOneBuckets() []vault.Name {
	return []vault.Name{terminalDeliveryBucket}
}
