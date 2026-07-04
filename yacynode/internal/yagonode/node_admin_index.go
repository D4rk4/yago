package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yacynode/internal/adminui"
	"github.com/D4rk4/yago/yacynode/internal/searchindex"
)

type indexSource struct {
	index searchindex.SearchIndex
}

func newIndexSource(index searchindex.SearchIndex) indexSource {
	return indexSource{index: index}
}

func (s indexSource) Index(ctx context.Context) adminui.IndexStats {
	if s.index == nil {
		return adminui.IndexStats{}
	}

	stats, err := s.index.Stats(ctx)
	if err != nil {
		return adminui.IndexStats{}
	}

	return adminui.IndexStats{
		Available: true,
		Documents: stats.Documents,
		Backend:   stats.Backend,
		UpdatedAt: formattedIndexStatsTime(stats.UpdatedAt),
	}
}
