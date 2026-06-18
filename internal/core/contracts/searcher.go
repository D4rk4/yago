package contracts

import (
	"context"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type SearchAbstractMode int

const (
	SearchAbstractNone SearchAbstractMode = iota
	SearchAbstractAuto
	SearchAbstractExplicit
)

type SearchAbstractRequest struct {
	Mode  SearchAbstractMode
	Words []yacymodel.Hash
}

type SearchFilters struct {
	ContentDomain    string
	StrictContentDom bool
	TimezoneOffset   int
	Language         string
	Modifier         string
	Prefer           string
	Filter           string
	Constraint       string
	Profile          string
	SiteHost         string
	SiteHash         string
	Author           string
	Collection       string
	FileType         string
	Protocol         string
	Partitions       int
}

type SearchQuery struct {
	Words       []yacymodel.Hash
	Exclude     []yacymodel.Hash
	URLs        []yacymodel.Hash
	MaxResults  int
	MaxDistance int
	MaxTime     time.Duration
	Abstracts   SearchAbstractRequest
	Filters     SearchFilters
}

type SearchResult struct {
	Resources  []yacymodel.URIMetadataRow
	JoinCount  int
	SearchTime time.Duration
	References []string
	WordCounts map[yacymodel.Hash]int
	Abstracts  map[yacymodel.Hash]string
}

type Searcher interface {
	Search(ctx context.Context, query SearchQuery) (SearchResult, error)
}
