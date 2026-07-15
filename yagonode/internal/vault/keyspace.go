package vault

import "fmt"

type Keyspace[V any] struct {
	name  Name
	codec Codec[V]
}

func RegisterKeyspace[V any](
	v *Vault,
	bucket Name,
	codec Codec[V],
) (*Keyspace[V], error) {
	if err := registerBucket(v, bucket); err != nil {
		return nil, err
	}

	return &Keyspace[V]{name: bucket, codec: codec}, nil
}

func (k *Keyspace[V]) Get(tx *Txn, key Key) (V, bool, error) {
	var zero V
	raw := tx.etx.Bucket(k.name).Get(key)
	if raw == nil {
		return zero, false, nil
	}
	value, err := k.codec.Decode(raw)
	if err != nil {
		return zero, false, fmt.Errorf("decode %s: %w", k.name, err)
	}

	return value, true, nil
}

func (k *Keyspace[V]) Contains(tx *Txn, key Key) bool {
	bucket := tx.etx.Bucket(k.name)
	if presence, ok := bucket.(interface{ Contains(Key) bool }); ok {
		return presence.Contains(key)
	}

	return bucket.Get(key) != nil
}

func (k *Keyspace[V]) Put(tx *Txn, key Key, value V) error {
	if !tx.etx.Writable() {
		return errReadOnly
	}
	raw, err := k.codec.Encode(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", k.name, err)
	}
	if err := tx.etx.Bucket(k.name).Put(key, raw); err != nil {
		return fmt.Errorf("store %s: %w", k.name, err)
	}

	return nil
}

func (k *Keyspace[V]) Delete(tx *Txn, key Key) (bool, error) {
	if !tx.etx.Writable() {
		return false, errReadOnly
	}
	bucket := tx.etx.Bucket(k.name)
	if bucket.Get(key) == nil {
		return false, nil
	}
	if err := bucket.Delete(key); err != nil {
		return false, fmt.Errorf("delete %s: %w", k.name, err)
	}

	return true, nil
}
