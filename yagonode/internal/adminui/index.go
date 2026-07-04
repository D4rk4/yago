package adminui

import "context"

// IndexStats is the search-index snapshot the Index section renders.
type IndexStats struct {
	Available bool
	Documents int
	Backend   string
	UpdatedAt string
}

// IndexSource supplies the search-index snapshot on each request.
type IndexSource interface {
	Index(ctx context.Context) IndexStats
}
