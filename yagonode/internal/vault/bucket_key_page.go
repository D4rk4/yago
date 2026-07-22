package vault

import (
	"bytes"
	"fmt"
)

type BucketKeyPage struct {
	Keys []Key
	More bool
}

type engineBucketKeyPageReader interface {
	ReadKeyPageAfter(Key, int) (BucketKeyPage, error)
}

func (t *Txn) ReadBucketKeyPage(name Name, after Key, limit int) (BucketKeyPage, error) {
	if limit < 1 {
		return BucketKeyPage{}, fmt.Errorf("read bucket %s key page: limit must be positive", name)
	}
	bucket := t.etx.Bucket(name)
	if reader, ok := bucket.(engineBucketKeyPageReader); ok {
		page, err := reader.ReadKeyPageAfter(after, limit)
		if err != nil {
			return BucketKeyPage{}, fmt.Errorf("read bucket %s key page: %w", name, err)
		}
		if err := validateBucketKeyPage(page, after, limit); err != nil {
			return BucketKeyPage{}, fmt.Errorf("read bucket %s key page: %w", name, err)
		}
		for index := range page.Keys {
			page.Keys[index] = append(Key(nil), page.Keys[index]...)
		}

		return page, nil
	}
	keys := make([]Key, 0, limit+1)
	if err := bucket.Scan(nil, func(key Key, _ []byte) (bool, error) {
		if after != nil && bytes.Compare(key, after) <= 0 {
			return true, nil
		}
		keys = append(keys, append(Key(nil), key...))

		return len(keys) <= limit, nil
	}); err != nil {
		return BucketKeyPage{}, fmt.Errorf("read bucket %s key page: %w", name, err)
	}
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}

	return BucketKeyPage{Keys: keys, More: more}, nil
}

func validateBucketKeyPage(page BucketKeyPage, after Key, limit int) error {
	if len(page.Keys) > limit {
		return fmt.Errorf("%w: keys exceed limit", errInvalidBucketPage)
	}
	if page.More && len(page.Keys) == 0 {
		return fmt.Errorf("%w: continuation without keys", errInvalidBucketPage)
	}
	previous := after
	hasPrevious := after != nil
	for index, key := range page.Keys {
		if hasPrevious && bytes.Compare(key, previous) <= 0 {
			return fmt.Errorf("%w: key %d did not advance", errInvalidBucketPage, index)
		}
		previous = key
		hasPrevious = true
	}

	return nil
}
