package vault

import (
	"context"
	"fmt"
)

func (k *Keyspace[V]) Values(
	ctx context.Context,
	tx *Txn,
	keys []Key,
) ([]V, []bool, error) {
	values := make([]V, len(keys))
	raw, found, err := readEncodedValues(ctx, tx, k.name, keys)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s values: %w", k.name, err)
	}
	for index := range keys {
		if !found[index] {
			continue
		}
		value, err := k.codec.Decode(raw[index])
		if err != nil {
			return nil, nil, corruptValueDecodeError(k.name, err)
		}
		values[index] = value
	}

	return values, found, nil
}

func (k *Keyspace[V]) Presence(
	ctx context.Context,
	tx *Txn,
	keys []Key,
) ([]bool, error) {
	found, err := readPresence(ctx, tx, k.name, keys)
	if err != nil {
		return nil, fmt.Errorf("read %s presence: %w", k.name, err)
	}

	return found, nil
}
