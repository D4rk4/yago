package searchindex

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type SearchIndex interface {
	Index(ctx context.Context, doc documentstore.Document) error
	Delete(ctx context.Context, docID string) error
	Search(ctx context.Context, req SearchRequest) (SearchResultSet, error)
	Stats(ctx context.Context) (IndexStats, error)
}

type SearchRequest struct {
	Query         string
	ExcludeTerms  []string
	Phrases       []string
	MaxResults    int
	IncludeRaw    bool
	IncludeDomain []string
	ExcludeDomain []string
	Language      string
	Since         time.Time
	Until         time.Time
	Weights       RankingWeights
	Explain       bool
	// Fuzzy widens the main field matches to edit-distance-1 term matching for
	// the zero-result recovery retry.
	Fuzzy bool
	// Author keeps only documents whose extracted author metadata contains this
	// text (case-insensitive).
	Author string
	// Terms carries the parsed query words for the proximity filter; Near keeps
	// only documents where every term appears within one small token window.
	Terms []string
	Near  bool
	// WithFacets asks for facet counts over every matching document.
	WithFacets bool
}

type SearchResultSet struct {
	// Facets carries the facet groups when the request asked for them.
	Facets  []FacetGroup
	Results []SearchResult
	Total   int
}

type SearchResult struct {
	DocumentID    string
	Title         string
	URL           string
	Snippet       string
	RawContent    string
	Score         float64
	Explanation   string
	PublishedDate time.Time
}

type IndexStats struct {
	Documents int
	Backend   string
	UpdatedAt time.Time
}
