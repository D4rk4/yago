package boltvault

import (
	"encoding/binary"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func Register[V any](v *Vault, bucket Name, codec Codec[V]) (*Collection[V], error) {
	if v == nil || v.db == nil {
		return nil, errVaultClosed
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, dup := v.registered[bucket]; dup {
		return nil, fmt.Errorf("%w: %s", errDuplicateBucket, bucket)
	}

	if err := v.db.Update(func(tx *bolt.Tx) error {
		if _, createErr := tx.CreateBucketIfNotExists([]byte(bucket)); createErr != nil {
			return fmt.Errorf("create bucket: %w", createErr)
		}
		lengths := tx.Bucket(lengthBucket)
		if lengths.Get([]byte(bucket)) == nil {
			return putLength(lengths, []byte(bucket), 0)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("register bucket %s: %w", bucket, err)
	}

	v.registered[bucket] = struct{}{}

	return &Collection[V]{vault: v, name: bucket, codec: codec}, nil
}

func readLength(tx *bolt.Tx, bucket Name) (int, error) {
	return decodeLength(tx.Bucket(lengthBucket).Get([]byte(bucket)))
}

func adjustLength(tx *bolt.Tx, bucket Name, delta int) error {
	lengths := tx.Bucket(lengthBucket)
	current, err := decodeLength(lengths.Get([]byte(bucket)))
	if err != nil {
		return err
	}

	return putLength(lengths, []byte(bucket), max(current+delta, 0))
}

func decodeLength(raw []byte) (int, error) {
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("bad length counter: %d bytes", len(raw))
	}

	n := binary.BigEndian.Uint64(raw)
	if n > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("length counter overflow")
	}

	return int(n), nil
}

func putLength(lengths *bolt.Bucket, key []byte, n int) error {
	if n < 0 {
		return errors.New("negative length counter")
	}

	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(n))
	if err := lengths.Put(key, raw[:]); err != nil {
		return fmt.Errorf("store length counter: %w", err)
	}

	return nil
}
