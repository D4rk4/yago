package boltvault

import (
	"bytes"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b boltBucket) ReadPageAfter(after vault.Key, limit int) (vault.BucketPage, error) {
	cursor := b.bucket.Cursor()
	key, value := cursor.First()
	if after != nil {
		key, value = cursor.Seek(after)
		if bytes.Equal(key, after) {
			key, value = cursor.Next()
		}
	}
	entries := make([]vault.BucketPageEntry, 0, limit)
	for key != nil && len(entries) < limit {
		entries = append(entries, vault.BucketPageEntry{Key: key, Value: value})
		key, value = cursor.Next()
	}

	return vault.BucketPage{Entries: entries, More: key != nil}, nil
}
