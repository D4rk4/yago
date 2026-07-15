package boltvault

import "github.com/D4rk4/yago/yagonode/internal/vault"

func (b boltBucket) LastKey() (vault.Key, error) {
	if b.bucket == nil {
		return nil, nil
	}
	key, _ := b.bucket.Cursor().Last()

	return key, nil
}
