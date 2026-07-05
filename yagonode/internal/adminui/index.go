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
