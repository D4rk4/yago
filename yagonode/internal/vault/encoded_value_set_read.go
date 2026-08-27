package vault

import (
	"context"
	"fmt"
)

type engineBucketValueSetReader interface {
	ReadValues(context.Context, []Key) ([][]byte, []bool, error)
}

type engineBucketPresenceReader interface {
	ReadPresence(context.Context, []Key) ([]bool, error)
}

func readEncodedValues(
	ctx context.Context,
	tx *Txn,
	name Name,
	keys []Key,
) ([][]byte, []bool, error) {
	bucket := tx.etx.Bucket(name)
	if reader, ok := bucket.(engineBucketValueSetReader); ok {
		values, found, err := reader.ReadValues(ctx, keys)
		if err != nil {
			return nil, nil, fmt.Errorf("read encoded values: %w", err)
		}
		if len(values) != len(keys) || len(found) != len(keys) {
			return nil, nil, fmt.Errorf(
				"encoded value results = %d/%d, want %d",
				len(values),
				len(found),
				len(keys),
			)
		}

		return values, found, nil
	}
	values := make([][]byte, len(keys))
	found := make([]bool, len(keys))
	for index, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("context: %w", err)
		}
		raw := bucket.Get(key)
		if raw == nil {
			continue
		}
		values[index] = raw
		found[index] = true
	}

	return values, found, nil
}

func readPresence(
	ctx context.Context,
	tx *Txn,
	name Name,
	keys []Key,
) ([]bool, error) {
	bucket := tx.etx.Bucket(name)
	if reader, ok := bucket.(engineBucketPresenceReader); ok {
		found, err := reader.ReadPresence(ctx, keys)
		if err != nil {
			return nil, fmt.Errorf("read presence: %w", err)
		}
		if len(found) != len(keys) {
			return nil, fmt.Errorf(
				"presence results = %d, want %d",
				len(found),
				len(keys),
			)
		}

		return found, nil
	}
	found := make([]bool, len(keys))
	for index, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context: %w", err)
		}
		if presence, ok := bucket.(interface{ Contains(Key) bool }); ok {
			found[index] = presence.Contains(key)
		} else {
			found[index] = bucket.Get(key) != nil
		}
	}

	return found, nil
}
