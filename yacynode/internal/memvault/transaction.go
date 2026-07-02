package memvault

import (
	"fmt"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type memTxn struct {
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

func (t memTxn) Writable() bool { return t.writable }

func (t memTxn) Bucket(name vault.Name) vault.EngineBucket {
	return memBucket{entries: t.buckets[name]}
}

type memBucket struct {
	entries map[string][]byte
}

func (b memBucket) Get(key vault.Key) []byte {
	value, ok := b.entries[string(key)]
	if !ok {
		return nil
	}

	return value
}

func (b memBucket) Put(key vault.Key, value []byte) error {
	b.entries[string(key)] = copyValue(value)

	return nil
}

func (b memBucket) Delete(key vault.Key) error {
	delete(b.entries, string(key))

	return nil
}

func (b memBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	ordered := make([]string, 0, len(b.entries))
	for key := range b.entries {
		if strings.HasPrefix(key, string(prefix)) {
			ordered = append(ordered, key)
		}
	}
	sort.Strings(ordered)

	for _, key := range ordered {
		keep, err := fn(vault.Key(key), b.entries[key])
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func snapshot(source map[vault.Name]map[string][]byte) map[vault.Name]map[string][]byte {
	copied := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		entries := make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			entries[key] = copyValue(value)
		}
		copied[name] = entries
	}

	return copied
}

func copyValue(value []byte) []byte {
	copied := make([]byte, len(value))
	copy(copied, value)

	return copied
}
