package boltvault

import (
	"bytes"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b boltBucket) ReadKeyPageAfter(after vault.Key, limit int) (vault.BucketKeyPage, error) {
	cursor := b.bucket.Cursor()
	key, _ := cursor.First()
	if after != nil {
		key, _ = cursor.Seek(after)
		if bytes.Equal(key, after) {
			key, _ = cursor.Next()
		}
	}
	keys := make([]vault.Key, 0, limit)
	for key != nil && len(keys) < limit {
		keys = append(keys, key)
		key, _ = cursor.Next()
	}

	return vault.BucketKeyPage{Keys: keys, More: key != nil}, nil
}
