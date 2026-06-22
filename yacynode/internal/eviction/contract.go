// Package eviction frees storage when the vault nears its quota. It owns no
// buckets: it reads usage from the storage kernel, asks urlmeta for the stalest
// URLs, and purges them from rwi and urlmeta within one capacity-exempt
// transaction, so both collections drop atomically without sharing a schema.
package eviction

type Config struct {
	TargetFraction float64
	BatchSize      int
}

type Result struct {
	URLsDeleted     int
	PostingsDeleted int
}
