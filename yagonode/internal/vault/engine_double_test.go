package vault_test

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type doubleEngine struct {
	buckets    map[vault.Name]map[string][]byte
	quotaBytes int64
}

func openDouble() (*vault.Vault, error) {
	v, err := vault.New(&doubleEngine{
		buckets: map[vault.Name]map[string][]byte{},
	})
	if err != nil {
		return nil, fmt.Errorf("new vault: %w", err)
	}

	return v, nil
}

func (e *doubleEngine) Provision(name vault.Name) error {
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *doubleEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	staged := snapshotBuckets(e.buckets)
	if err := fn(doubleTxn{buckets: staged, writable: true}); err != nil {
		return err
	}
	e.buckets = staged

	return nil
}

func (e *doubleEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	return fn(doubleTxn{buckets: e.buckets, writable: false})
}

func (e *doubleEngine) Close() error {
	e.buckets = nil

	return nil
}

func (e *doubleEngine) QuotaBytes() int64 { return e.quotaBytes }

func (e *doubleEngine) UsedBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context: %w", err)
	}

	var used int64
	for _, bucket := range e.buckets {
		for key, value := range bucket {
			used += int64(len(key) + len(value))
		}
	}

	return used, nil
}

type doubleTxn struct {
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

func (t doubleTxn) Writable() bool { return t.writable }

func (t doubleTxn) Bucket(name vault.Name) vault.EngineBucket {
	return doubleBucket{entries: t.buckets[name]}
}

type doubleBucket struct {
	entries map[string][]byte
}

func (b doubleBucket) Get(key vault.Key) []byte {
	value, ok := b.entries[string(key)]
	if !ok {
		return nil
	}

	return copyBytes(value)
}

func (b doubleBucket) Put(key vault.Key, value []byte) error {
	b.entries[string(key)] = copyBytes(value)

	return nil
}

func (b doubleBucket) Delete(key vault.Key) error {
	delete(b.entries, string(key))

	return nil
}

func (b doubleBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	keys := make([]string, 0, len(b.entries))
	for key := range b.entries {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		keep, err := fn(vault.Key(key), copyBytes(b.entries[key]))
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func snapshotBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	staged := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		entries := make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			entries[key] = copyBytes(value)
		}
		staged[name] = entries
	}

	return staged
}

func copyBytes(value []byte) []byte {
	out := make([]byte, len(value))
	copy(out, value)

	return out
}
