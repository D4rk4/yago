package vault

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	errBucketPageUnavailable = errors.New("bucket page unavailable")
	errInvalidBucketPage     = errors.New("invalid bucket page")
)

type BucketPageEntry struct {
	Key   Key
	Value []byte
}

type BucketPage struct {
	Entries []BucketPageEntry
	More    bool
}

type engineBucketPageReader interface {
	ReadPageAfter(Key, int) (BucketPage, error)
}

func (t *Txn) ReadBucketPage(name Name, after Key, limit int) (BucketPage, error) {
	if limit < 1 {
		return BucketPage{}, fmt.Errorf("read bucket %s page: limit must be positive", name)
	}
	reader, ok := t.etx.Bucket(name).(engineBucketPageReader)
	if !ok {
		return BucketPage{}, fmt.Errorf("read bucket %s page: %w", name, errBucketPageUnavailable)
	}
	page, err := reader.ReadPageAfter(after, limit)
	if err != nil {
		return BucketPage{}, fmt.Errorf("read bucket %s page: %w", name, err)
	}
	if err := validateBucketPage(page, after, limit); err != nil {
		return BucketPage{}, fmt.Errorf("read bucket %s page: %w", name, err)
	}
	for index := range page.Entries {
		page.Entries[index].Key = append(Key(nil), page.Entries[index].Key...)
		page.Entries[index].Value = append([]byte(nil), page.Entries[index].Value...)
	}

	return page, nil
}

func validateBucketPage(page BucketPage, after Key, limit int) error {
	if len(page.Entries) > limit {
		return fmt.Errorf("%w: entries exceed limit", errInvalidBucketPage)
	}
	if page.More && len(page.Entries) == 0 {
		return fmt.Errorf("%w: continuation without entries", errInvalidBucketPage)
	}
	previous := after
	hasPrevious := after != nil
	for index, entry := range page.Entries {
		if hasPrevious && bytes.Compare(entry.Key, previous) <= 0 {
			return fmt.Errorf("%w: entry %d did not advance", errInvalidBucketPage, index)
		}
		previous = entry.Key
		hasPrevious = true
	}

	return nil
}
