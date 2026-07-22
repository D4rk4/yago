package shardvault

import (
	"container/heap"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *shardBucket) ReadKeyPageAfter(after vault.Key, limit int) (vault.BucketKeyPage, error) {
	cursors, err := b.openCursorsAfter(after)
	if err != nil {
		return vault.BucketKeyPage{}, err
	}
	heap.Init(&cursors)
	keys := make([]vault.Key, 0, limit)
	for cursors.Len() > 0 && len(keys) < limit {
		head := heap.Pop(&cursors).(*shardCursor)
		keys = append(keys, head.key)
		key, raw := head.cursor.Next()
		key, raw = b.advancePastForeignEntries(head.shardIndex, head.cursor, key, raw)
		if key != nil {
			head.key, head.raw = key, raw
			heap.Push(&cursors, head)
		}
	}

	return vault.BucketKeyPage{Keys: keys, More: cursors.Len() > 0}, nil
}
