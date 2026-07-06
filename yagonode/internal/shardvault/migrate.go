package shardvault

import (
	"context"
	"fmt"
	"os"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const migratedSuffix = ".migrated.bak"

// OpenAt opens the sharded vault that lives beside the configured single-file
// storage path. A legacy single-file vault found at the path streams into the
// sharded layout once (compressing on the way) and is kept as a .migrated.bak
// file until the operator removes it.
func OpenAt(legacyPath string, quotaBytes int64) (*vault.Vault, error) {
	shardEngine, err := openEngine(legacyPath+".vault", quotaBytes)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(legacyPath); err == nil {
		if err := migrateLegacy(legacyPath, shardEngine); err != nil {
			closeShards(shardEngine.shards)

			return nil, fmt.Errorf("migrate legacy vault: %w", err)
		}
		if err := os.Rename(legacyPath, legacyPath+migratedSuffix); err != nil {
			closeShards(shardEngine.shards)

			return nil, fmt.Errorf("retire legacy vault: %w", err)
		}
	}

	return vaultOverEngine(shardEngine)
}

// migrateLegacy streams every bucket and record of the legacy bbolt file into
// the sharded engine, which compresses values on the way in.
func migrateLegacy(legacyPath string, target *engine) error {
	legacy, err := bolt.Open(legacyPath, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("open legacy vault: %w", err)
	}
	defer func() { _ = legacy.Close() }()

	return legacy.View(func(tx *bolt.Tx) error { //nolint:wrapcheck // wrapped by OpenAt.
		return tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
			return copyLegacyBucket(target, vault.Name(name), bucket)
		})
	})
}

// copyLegacyBucket provisions the bucket on the sharded engine and copies its
// records through the compressing engine surface.
func copyLegacyBucket(target *engine, name vault.Name, bucket *bolt.Bucket) error {
	if err := target.Provision(name); err != nil {
		return err
	}
	ctx := context.Background()

	return bucket.ForEach(func(key, value []byte) error { //nolint:wrapcheck // self-describing.
		return target.Update(ctx, func(txn vault.EngineTxn) error {
			return txn.Bucket(name).Put(append(vault.Key{}, key...), append([]byte{}, value...))
		})
	})
}
