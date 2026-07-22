package vault

import "fmt"

type engineBucketValueReader interface {
	ReadValue(Key) ([]byte, bool, error)
}

func readEncodedValue(tx *Txn, name Name, key Key) ([]byte, bool, error) {
	bucket := tx.etx.Bucket(name)
	if reader, ok := bucket.(engineBucketValueReader); ok {
		raw, found, err := reader.ReadValue(key)
		if err != nil {
			return nil, found, fmt.Errorf("read encoded value: %w", err)
		}

		return raw, found, nil
	}
	raw := bucket.Get(key)
	if raw == nil {
		return nil, false, nil
	}

	return raw, true, nil
}
