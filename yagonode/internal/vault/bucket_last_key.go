package vault

import (
	"errors"
	"fmt"
)

var errBucketLastKeyUnavailable = errors.New("bucket last key unavailable")

type engineBucketLastKeyReader interface {
	LastKey() (Key, error)
}

func (t *Txn) ReadBucketLastKey(name Name) (Key, error) {
	reader, ok := t.etx.Bucket(name).(engineBucketLastKeyReader)
	if !ok {
		return nil, fmt.Errorf("read bucket %s last key: %w", name, errBucketLastKeyUnavailable)
	}
	key, err := reader.LastKey()
	if err != nil {
		return nil, fmt.Errorf("read bucket %s last key: %w", name, err)
	}

	return append(Key(nil), key...), nil
}
