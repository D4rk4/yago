package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIStore interface {
	AppendRWI(ctx context.Context, entries []yacymodel.RWIEntry) ([]yacymodel.Hash, error)
	SearchPostings(
		ctx context.Context,
		query PostingSearchQuery,
	) (PostingSearchResult, error)
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}

type PostingSearchQuery struct {
	WordHashes    []yacymodel.Hash
	ExcludeHashes []yacymodel.Hash
	URLHashes     []yacymodel.Hash
	LimitPerWord  int
	MaxDistance   int
	Language      string
}

type PostingSearchResult struct {
	Postings  map[yacymodel.Hash][]yacymodel.RWIEntry
	Counts    map[yacymodel.Hash]int
	Truncated bool
}
