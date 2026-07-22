package boltvault

import "github.com/D4rk4/yago/yagonode/internal/vault"

func (b boltBucket) ValueSize(key vault.Key) (int, bool, error) {
	value := b.bucket.Get(key)
	if value == nil {
		return 0, false, nil
	}

	return len(value), true, nil
}
