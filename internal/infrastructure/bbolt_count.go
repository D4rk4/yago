package infrastructure

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

func (s *BboltStorage) count(ctx context.Context, key []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}

	var n int
	err := s.view(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketCounts).Get(key)
		count, err := decodeCount(value)
		if err != nil {
			return err
		}
		n = count

		return nil
	})
	if err != nil {
		return 0, err
	}

	return n, nil
}

func incrementCount(bucket *bolt.Bucket, key []byte) error {
	n, err := decodeCount(bucket.Get(key))
	if err != nil {
		return err
	}

	return putCount(bucket, key, n+1)
}

func decodeCount(raw []byte) (int, error) {
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("bad count length: %d", len(raw))
	}

	n := binary.BigEndian.Uint64(raw)
	if n > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("count overflow")
	}

	return int(n), nil
}

func putCount(bucket *bolt.Bucket, key []byte, n int) error {
	value, err := countUint64(n)
	if err != nil {
		return err
	}

	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	if err := bucket.Put(key, raw[:]); err != nil {
		return fmt.Errorf("store count: %w", err)
	}

	return nil
}

func countUint64(n int) (uint64, error) {
	if n < 0 {
		return 0, errors.New("negative count")
	}

	value, err := strconv.ParseUint(strconv.Itoa(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("encode count: %w", err)
	}

	return value, nil
}
