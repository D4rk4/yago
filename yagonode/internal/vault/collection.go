package vault

import "fmt"

type Collection[V any] struct {
	vault *Vault
	name  Name
	codec Codec[V]
}

func (c *Collection[V]) Get(tx *Txn, key Key) (V, bool, error) {
	var zero V

	raw := tx.etx.Bucket(c.name).Get(key)
	if raw == nil {
		return zero, false, nil
	}

	val, err := c.codec.Decode(raw)
	if err != nil {
		return zero, false, fmt.Errorf("decode %s: %w", c.name, err)
	}

	return val, true, nil
}

func (c *Collection[V]) Contains(tx *Txn, key Key) bool {
	bucket := tx.etx.Bucket(c.name)
	if presence, ok := bucket.(interface{ Contains(Key) bool }); ok {
		return presence.Contains(key)
	}

	return bucket.Get(key) != nil
}

func (c *Collection[V]) Put(tx *Txn, key Key, val V) error {
	if !tx.etx.Writable() {
		return errReadOnly
	}

	raw, err := c.codec.Encode(val)
	if err != nil {
		return fmt.Errorf("encode %s: %w", c.name, err)
	}

	bucket := tx.etx.Bucket(c.name)
	existed := bucket.Get(key) != nil
	if err := bucket.Put(key, raw); err != nil {
		return fmt.Errorf("store %s: %w", c.name, err)
	}
	if existed {
		return nil
	}

	return adjustLength(tx, c.name, 1)
}

func (c *Collection[V]) Delete(tx *Txn, key Key) (bool, error) {
	if !tx.etx.Writable() {
		return false, errReadOnly
	}

	bucket := tx.etx.Bucket(c.name)
	if bucket.Get(key) == nil {
		return false, nil
	}
	if err := bucket.Delete(key); err != nil {
		return false, fmt.Errorf("delete %s: %w", c.name, err)
	}
	if err := adjustLength(tx, c.name, -1); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Collection[V]) Scan(tx *Txn, prefix Key, fn func(Key, V) (bool, error)) error {
	if err := tx.etx.Bucket(c.name).Scan(prefix, func(key Key, raw []byte) (bool, error) {
		val, err := c.codec.Decode(raw)
		if err != nil {
			return false, fmt.Errorf("decode %s: %w", c.name, err)
		}

		return fn(append(Key(nil), key...), val)
	}); err != nil {
		return fmt.Errorf("scan %s: %w", c.name, err)
	}

	return nil
}

func (c *Collection[V]) Len(tx *Txn) (int, error) {
	return readLength(tx, c.name)
}
