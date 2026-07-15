package shardvault

import (
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *shardBucket) advancePastForeignEntries(
	shardIndex int,
	cursor *bolt.Cursor,
	key vault.Key,
	storedValue []byte,
) (vault.Key, []byte) {
	for key != nil && b.txn.engine.route(b.name, key) != shardIndex {
		key, storedValue = cursor.Next()
	}

	return key, storedValue
}

func (b *shardBucket) retreatPastForeignEntries(
	shardIndex int,
	cursor *bolt.Cursor,
	key vault.Key,
	storedValue []byte,
) (vault.Key, []byte) {
	for key != nil && b.txn.engine.route(b.name, key) != shardIndex {
		key, storedValue = cursor.Prev()
	}

	return key, storedValue
}
