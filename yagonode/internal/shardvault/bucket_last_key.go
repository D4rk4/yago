package shardvault

import (
	"bytes"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *shardBucket) LastKey() (vault.Key, error) {
	var last vault.Key
	for shardIndex := range b.txn.engine.shards {
		tx, err := b.txn.shard(shardIndex)
		if err != nil {
			return nil, err
		}
		bucket := tx.Bucket([]byte(b.name))
		if bucket == nil {
			continue
		}
		cursor := bucket.Cursor()
		key, storedValue := cursor.Last()
		key, _ = b.retreatPastForeignEntries(
			shardIndex,
			cursor,
			key,
			storedValue,
		)
		if key != nil && (last == nil || bytes.Compare(key, last) > 0) {
			last = append(vault.Key(nil), key...)
		}
	}

	return last, nil
}
