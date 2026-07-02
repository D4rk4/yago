package searchindex

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/documentstore"
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
	MaxResults    int
	IncludeRaw    bool
	IncludeDomain []string
	ExcludeDomain []string
	Language      string
	Since         time.Time
	Until         time.Time
}

type SearchResultSet struct {
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
	PublishedDate time.Time
}

type IndexStats struct {
	Documents int
	Backend   string
	UpdatedAt time.Time
}
