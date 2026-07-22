package vault

import "fmt"

type engineBucketValueSizer interface {
	ValueSize(Key) (int, bool, error)
}

func (c *Collection[V]) EncodedSize(tx *Txn, key Key) (int, bool, error) {
	return encodedValueSize(tx, c.name, key)
}

func encodedValueSize(tx *Txn, name Name, key Key) (int, bool, error) {
	bucket := tx.etx.Bucket(name)
	if sizer, ok := bucket.(engineBucketValueSizer); ok {
		size, found, err := sizer.ValueSize(key)
		if err != nil {
			return size, found, fmt.Errorf("measure encoded value: %w", err)
		}

		return size, found, nil
	}
	raw := bucket.Get(key)
	if raw == nil {
		return 0, false, nil
	}

	return len(raw), true, nil
}
