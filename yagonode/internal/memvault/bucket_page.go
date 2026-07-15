package memvault

import (
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b memBucket) ReadPageAfter(after vault.Key, limit int) (vault.BucketPage, error) {
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
	entries := make([]vault.BucketPageEntry, 0, end-start)
	for _, key := range ordered[start:end] {
		entries = append(entries, vault.BucketPageEntry{
			Key:   vault.Key(key),
			Value: b.entries[key],
		})
	}

	return vault.BucketPage{Entries: entries, More: end < len(ordered)}, nil
}
