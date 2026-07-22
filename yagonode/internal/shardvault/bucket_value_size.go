package shardvault

import "github.com/D4rk4/yago/yagonode/internal/vault"

func (b *shardBucket) ValueSize(key vault.Key) (int, bool, error) {
	bucket, err := b.boltBucketFor(key)
	if err != nil {
		return 0, false, err
	}
	stored := bucket.Get(key)
	if stored == nil {
		return 0, false, nil
	}
	size, err := storedValueSize(stored)
	if err != nil {
		return 0, true, err
	}

	return size, true, nil
}
