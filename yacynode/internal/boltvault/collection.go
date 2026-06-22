package boltvault

import (
	"bytes"
	"fmt"
)

type Collection[V any] struct {
	vault *Vault
	name  Name
	codec Codec[V]
}

func (c *Collection[V]) Get(tx *Txn, key Key) (V, bool, error) {
	var zero V

	raw := tx.tx.Bucket([]byte(c.name)).Get(key)
	if raw == nil {
		return zero, false, nil
	}

	val, err := c.codec.Decode(raw)
	if err != nil {
		return zero, false, fmt.Errorf("decode %s: %w", c.name, err)
	}

	return val, true, nil
}

func (c *Collection[V]) Put(tx *Txn, key Key, val V) error {
	if !tx.writable {
		return errReadOnly
	}

	raw, err := c.codec.Encode(val)
	if err != nil {
		return fmt.Errorf("encode %s: %w", c.name, err)
	}

	bucket := tx.tx.Bucket([]byte(c.name))
	existed := bucket.Get(key) != nil
	if err := bucket.Put(key, raw); err != nil {
		return fmt.Errorf("store %s: %w", c.name, err)
	}
	if existed {
		return nil
	}

	return adjustLength(tx.tx, c.name, 1)
}

func (c *Collection[V]) Delete(tx *Txn, key Key) (bool, error) {
	if !tx.writable {
		return false, errReadOnly
	}

	bucket := tx.tx.Bucket([]byte(c.name))
	if bucket.Get(key) == nil {
		return false, nil
	}
	if err := bucket.Delete(key); err != nil {
		return false, fmt.Errorf("delete %s: %w", c.name, err)
	}
	if err := adjustLength(tx.tx, c.name, -1); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Collection[V]) Scan(tx *Txn, prefix Key, fn func(Key, V) (bool, error)) error {
	cursor := tx.tx.Bucket([]byte(c.name)).Cursor()

	var key, raw []byte
	if len(prefix) == 0 {
		key, raw = cursor.First()
	} else {
		key, raw = cursor.Seek(prefix)
	}

	for ; key != nil; key, raw = cursor.Next() {
		if len(prefix) > 0 && !bytes.HasPrefix(key, prefix) {
			break
		}

		val, err := c.codec.Decode(raw)
		if err != nil {
			return fmt.Errorf("decode %s: %w", c.name, err)
		}

		keep, err := fn(append(Key(nil), key...), val)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func (c *Collection[V]) Len(tx *Txn) (int, error) {
	return readLength(tx.tx, c.name)
}
