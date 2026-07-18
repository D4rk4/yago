package adminui

import "context"

// IndexStats is the search-index snapshot the Index section renders. The disk
// figures are prerendered human-readable strings; an empty string hides the row.
type IndexStats struct {
	Available  bool
	Documents  int
	Backend    string
	UpdatedAt  string
	DiskSize   string
	VaultUsed  string
	VaultQuota string
}

// IndexSource supplies the search-index snapshot on each request.
type IndexSource interface {
	Index(ctx context.Context) IndexStats
}

// CompactionResult is the outcome of a manual "Compact now" run: how many shards
// were rewritten and the space handed back to the OS as a prerendered string.
type CompactionResult struct {
	ShardsCompacted int
	BytesReclaimed  string
}

// Compactor rewrites over-full storage shards on demand so space freed by
// deletions is returned to the OS. The Configuration page's Storage tab exposes
// it as a "Compact now" button beside the compaction-interval setting.
type Compactor interface {
	Compact(ctx context.Context) (CompactionResult, error)
}

type CompactionOperatorError interface {
	CompactionOperatorMessage() string
}
