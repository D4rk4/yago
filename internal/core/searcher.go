package core

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type SearchQuery struct {
	Words       []yacymodel.Hash
	Exclude     []yacymodel.Hash
	URLs        []yacymodel.Hash
	MaxResults  int
	MaxDistance int
}

type SearchResult struct {
	Resources  []yacymodel.URIMetadataRow
	JoinCount  int
	WordCounts map[yacymodel.Hash]int
	Abstracts  map[yacymodel.Hash]string
}

type Searcher interface {
	Search(ctx context.Context, query SearchQuery) (SearchResult, error)
}
