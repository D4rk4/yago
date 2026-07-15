package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type overviewLocalDocuments struct {
	index searchindex.SearchIndex
}

func (s overviewSource) withLocalIndex(index searchindex.SearchIndex) overviewSource {
	s.localDocuments.index = index

	return s
}

func (d overviewLocalDocuments) read(ctx context.Context) (int, bool) {
	if d.index == nil {
		return 0, false
	}
	stats, err := d.index.Stats(ctx)
	if err != nil {
		return 0, false
	}

	return stats.Documents, true
}
