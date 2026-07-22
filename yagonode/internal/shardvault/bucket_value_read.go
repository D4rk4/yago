package shardvault

import (
	"errors"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *shardBucket) ReadValue(key vault.Key) ([]byte, bool, error) {
	bucket, err := b.boltBucketFor(key)
	if err != nil {
		b.latchReadValueError(err)

		return nil, false, err
	}
	stored := bucket.Get(key)
	if stored == nil {
		return nil, false, nil
	}
	value, err := decodeValue(stored)
	if err != nil {
		b.latchReadValueError(err)

		return nil, true, err
	}

	return value, true, nil
}

func (b *shardBucket) latchReadValueError(err error) {
	if !errors.Is(err, vault.ErrCorruptValue) {
		b.txn.latchAccessError(err)
	}
}
