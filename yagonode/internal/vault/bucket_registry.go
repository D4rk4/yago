package vault

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func Register[V any](v *Vault, bucket Name, codec Codec[V]) (*Collection[V], error) {
	if err := registerBucket(v, bucket); err != nil {
		return nil, err
	}

	return &Collection[V]{vault: v, name: bucket, codec: codec}, nil
}

func registerBucket(v *Vault, bucket Name) error {
	if v == nil {
		return errVaultClosed
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if _, dup := v.registered[bucket]; dup {
		return fmt.Errorf("%w: %s", errDuplicateBucket, bucket)
	}

	lease, err := v.acquireEngineLease()
	if err != nil {
		return err
	}
	defer lease.release()
	if err := lease.engine.Provision(bucket); err != nil {
		return fmt.Errorf("register bucket %s: %w", bucket, err)
	}

	v.registered[bucket] = struct{}{}

	return nil
}

func readLength(tx *Txn, bucket Name) (int, error) {
	return decodeLength(tx.etx.Bucket(lengthBucket).Get(Key(bucket)))
}

func adjustLength(tx *Txn, bucket Name, delta int) error {
	lengths := tx.etx.Bucket(lengthBucket)
	current, err := decodeLength(lengths.Get(Key(bucket)))
	if err != nil {
		return err
	}

	return putLength(lengths, Key(bucket), max(current+delta, 0))
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

func putLength(lengths EngineBucket, key Key, n int) error {
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
