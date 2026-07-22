package memvault

import "github.com/D4rk4/yago/yagonode/internal/vault"

func (b memBucket) ValueSize(key vault.Key) (int, bool, error) {
	value, found := b.entries[string(key)]
	if !found {
		return 0, false, nil
	}

	return len(value), true, nil
}
