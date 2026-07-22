package memvault

import (
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b memBucket) ReadKeyPageAfter(after vault.Key, limit int) (vault.BucketKeyPage, error) {
	ordered := make([]string, 0, len(b.entries))
	for key := range b.entries {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	start := 0
	if after != nil {
		start = sort.Search(len(ordered), func(index int) bool {
			return ordered[index] > string(after)
		})
	}
	end := min(start+limit, len(ordered))
	keys := make([]vault.Key, 0, end-start)
	for _, key := range ordered[start:end] {
		keys = append(keys, vault.Key(key))
	}

	return vault.BucketKeyPage{Keys: keys, More: end < len(ordered)}, nil
}
