package shardvault

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (e *engine) BucketProvisioned(ctx context.Context, name vault.Name) (bool, error) {
	if err := acquireGlobalRead(ctx, &e.globalGate); err != nil {
		return false, err
	}
	defer e.globalGate.RUnlock()
	presentShards := 0
	for shard, database := range e.shards {
		present := false
		if err := database.View(func(tx *bolt.Tx) error {
			present = tx.Bucket([]byte(name)) != nil

			return nil
		}); err != nil {
			return false, fmt.Errorf("inspect bucket %s on shard %d: %w", name, shard, err)
		}
		if present {
			presentShards++
		}
	}
	if presentShards != 0 && presentShards != len(e.shards) {
		return false, fmt.Errorf(
			"inspect bucket %s: present on %d of %d shards",
			name,
			presentShards,
			len(e.shards),
		)
	}

	return presentShards == len(e.shards), nil
}
