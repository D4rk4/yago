package shardvault

import (
	"encoding/binary"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const collectionLengthChangesBucket = "__collection_length_changes__"

var (
	createCollectionLengthChangesBucket = (*bolt.Tx).CreateBucketIfNotExists
	storeCollectionLengthChanges        = (*bolt.Bucket).Put
)

func (t *shardTxn) RecordCollectionAddition(
	collection vault.Name,
	record vault.Key,
) error {
	return t.recordCollectionLengthChange(collection, record, true)
}

func (t *shardTxn) RecordCollectionRemoval(
	collection vault.Name,
	record vault.Key,
) error {
	return t.recordCollectionLengthChange(collection, record, false)
}

func (t *shardTxn) recordCollectionLengthChange(
	collection vault.Name,
	record vault.Key,
	addition bool,
) error {
	shard := t.engine.route(collection, record)
	tx, err := t.shard(shard)
	if err != nil {
		return err
	}
	changes, err := createCollectionLengthChangesBucket(tx, []byte(collectionLengthChangesBucket))
	if err != nil {
		return fmt.Errorf("create collection length changes: %w", err)
	}
	additions, removals, err := decodeCollectionLengthChanges(changes.Get([]byte(collection)))
	if err != nil {
		return err
	}
	if addition {
		if additions == ^uint64(0) {
			return errors.New("collection length additions overflow")
		}
		additions++
	} else {
		if removals == ^uint64(0) {
			return errors.New("collection length removals overflow")
		}
		removals++
	}
	if err := storeCollectionLengthChanges(
		changes,
		[]byte(collection),
		encodeCollectionLengthChanges(additions, removals),
	); err != nil {
		return fmt.Errorf("store collection length changes: %w", err)
	}

	return nil
}

func (t *shardTxn) CollectionLengthChanges(collection vault.Name) (int, int, error) {
	var additions uint64
	var removals uint64
	for shard := range t.engine.shards {
		tx, err := t.shard(shard)
		if err != nil {
			return 0, 0, err
		}
		changes := tx.Bucket([]byte(collectionLengthChangesBucket))
		if changes == nil {
			continue
		}
		shardAdditions, shardRemovals, err := decodeCollectionLengthChanges(
			changes.Get([]byte(collection)),
		)
		if err != nil {
			return 0, 0, err
		}
		if ^uint64(0)-additions < shardAdditions || ^uint64(0)-removals < shardRemovals {
			return 0, 0, errors.New("collection length changes overflow")
		}
		additions += shardAdditions
		removals += shardRemovals
	}
	maximum := uint64(^uint(0) >> 1)
	if additions > maximum || removals > maximum {
		return 0, 0, errors.New("collection length changes overflow")
	}

	return int(additions), int(removals), nil
}

func decodeCollectionLengthChanges(raw []byte) (uint64, uint64, error) {
	if raw == nil {
		return 0, 0, nil
	}
	if len(raw) != 16 {
		return 0, 0, fmt.Errorf("bad collection length changes: %d bytes", len(raw))
	}

	return binary.BigEndian.Uint64(raw[:8]), binary.BigEndian.Uint64(raw[8:]), nil
}

func encodeCollectionLengthChanges(additions, removals uint64) []byte {
	raw := make([]byte, 16)
	binary.BigEndian.PutUint64(raw[:8], additions)
	binary.BigEndian.PutUint64(raw[8:], removals)

	return raw
}
