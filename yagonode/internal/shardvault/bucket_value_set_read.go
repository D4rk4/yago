package shardvault

import (
	"context"
	"errors"
	"fmt"
	"sync"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type shardKeyPosition struct {
	key      vault.Key
	position int
}

type shardKeySet struct {
	bucket    *bolt.Bucket
	positions []shardKeyPosition
}

type shardStoredValue struct {
	raw   []byte
	found bool
}

func (b *shardBucket) ReadValues(
	ctx context.Context,
	keys []vault.Key,
) ([][]byte, []bool, error) {
	sets, err := b.prepareShardKeySets(ctx, keys)
	if err != nil {
		return nil, nil, b.finishShardSetRead(err)
	}
	stored, err := readShardKeySets(ctx, sets, len(keys), func(
		bucket *bolt.Bucket,
		key vault.Key,
	) (shardStoredValue, error) {
		raw := bucket.Get(key)
		if raw == nil {
			return shardStoredValue{}, nil
		}
		value, err := decodeValue(raw)
		if err != nil {
			return shardStoredValue{}, err
		}

		return shardStoredValue{raw: value, found: true}, nil
	})
	if err != nil {
		return nil, nil, b.finishShardSetRead(err)
	}
	values := make([][]byte, len(stored))
	found := make([]bool, len(stored))
	for index, value := range stored {
		values[index] = value.raw
		found[index] = value.found
	}

	return values, found, nil
}

func (b *shardBucket) ReadPresence(
	ctx context.Context,
	keys []vault.Key,
) ([]bool, error) {
	sets, err := b.prepareShardKeySets(ctx, keys)
	if err != nil {
		return nil, b.finishShardSetRead(err)
	}
	found, err := readShardKeySets(ctx, sets, len(keys), func(
		bucket *bolt.Bucket,
		key vault.Key,
	) (bool, error) {
		return bucket.Get(key) != nil, nil
	})
	if err != nil {
		return nil, b.finishShardSetRead(err)
	}

	return found, nil
}

func (b *shardBucket) prepareShardKeySets(
	ctx context.Context,
	keys []vault.Key,
) ([]shardKeySet, error) {
	sets := make([]shardKeySet, len(b.txn.engine.shards))
	for position, key := range keys {
		shard := b.txn.engine.route(b.name, key)
		sets[shard].positions = append(sets[shard].positions, shardKeyPosition{
			key:      key,
			position: position,
		})
	}
	for shard := range sets {
		if len(sets[shard].positions) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context: %w", err)
		}
		tx, err := b.txn.shard(shard)
		if err != nil {
			return nil, err
		}
		sets[shard].bucket = tx.Bucket([]byte(b.name))
		if sets[shard].bucket == nil {
			return nil, fmt.Errorf("bucket %s not provisioned", b.name)
		}
	}

	return sets, nil
}

func readShardKeySets[V any](
	ctx context.Context,
	sets []shardKeySet,
	total int,
	read func(*bolt.Bucket, vault.Key) (V, error),
) ([]V, error) {
	values := make([]V, total)
	failures := make([]error, len(sets))
	var readers sync.WaitGroup
	for shard := range sets {
		set := sets[shard]
		if len(set.positions) == 0 {
			continue
		}
		readers.Go(func() {
			for _, entry := range set.positions {
				if err := ctx.Err(); err != nil {
					failures[shard] = fmt.Errorf("context: %w", err)

					return
				}
				value, err := read(set.bucket, entry.key)
				if err != nil {
					failures[shard] = err

					return
				}
				values[entry.position] = value
			}
		})
	}
	readers.Wait()
	for _, err := range failures {
		if err != nil {
			return nil, err
		}
	}

	return values, nil
}

func (b *shardBucket) finishShardSetRead(err error) error {
	if err != nil && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		b.latchReadValueError(err)
	}

	return err
}
