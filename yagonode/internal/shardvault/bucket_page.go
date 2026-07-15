package shardvault

import (
	"bytes"
	"container/heap"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *shardBucket) ReadPageAfter(after vault.Key, limit int) (vault.BucketPage, error) {
	cursors, err := b.openCursorsAfter(after)
	if err != nil {
		return vault.BucketPage{}, err
	}
	heap.Init(&cursors)
	entries := make([]vault.BucketPageEntry, 0, limit)
	for cursors.Len() > 0 && len(entries) < limit {
		head, _ := heap.Pop(&cursors).(*shardCursor)
		value, err := decodeValue(head.raw)
		if err != nil {
			return vault.BucketPage{}, fmt.Errorf("decode stored value: %w", err)
		}
		entries = append(entries, vault.BucketPageEntry{Key: head.key, Value: value})
		key, raw := head.cursor.Next()
		key, raw = b.advancePastForeignEntries(head.shardIndex, head.cursor, key, raw)
		if key != nil {
			head.key, head.raw = key, raw
			heap.Push(&cursors, head)
		}
	}

	return vault.BucketPage{Entries: entries, More: cursors.Len() > 0}, nil
}

func (b *shardBucket) openCursorsAfter(after vault.Key) (scanHeap, error) {
	cursors := make(scanHeap, 0, len(b.txn.engine.shards))
	for index := range b.txn.engine.shards {
		tx, err := b.txn.shard(index)
		if err != nil {
			return nil, err
		}
		bucket := tx.Bucket([]byte(b.name))
		if bucket == nil {
			continue
		}
		cursor := bucket.Cursor()
		key, raw := firstKeyAfter(cursor, after)
		key, raw = b.advancePastForeignEntries(index, cursor, key, raw)
		if key != nil {
			cursors = append(cursors, &shardCursor{
				shardIndex: index,
				cursor:     cursor,
				key:        key,
				raw:        raw,
			})
		}
	}

	return cursors, nil
}

func firstKeyAfter(cursor *bolt.Cursor, after vault.Key) ([]byte, []byte) {
	if after == nil {
		return cursor.First()
	}
	key, raw := cursor.Seek(after)
	if bytes.Equal(key, after) {
		return cursor.Next()
	}

	return key, raw
}
