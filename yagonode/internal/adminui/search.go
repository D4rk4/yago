package adminui

import (
	"context"
	"html/template"
)

// SearchQuery is an admin-console search request. Offset and Limit carry the
// requested result window so the console can page through matches.
type SearchQuery struct {
	Query  string
	Global bool
	Offset int
	Limit  int
}

// SearchPagination carries the prev/next navigation for a results page. The URLs
// are built server-side (query-encoded, scope preserved) so the template never
// assembles a URL from parts.
type SearchPagination struct {
	Page    int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

// SearchResult is one rendered hit. Source labels where the hit came from
// ("local", "peer", or "web") and renders in the result's metadata line.
type SearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
	// SnippetHTML carries the query-term-highlighted snippet (escaped text
	// plus <mark> only); when set it renders instead of the plain snippet.
	SnippetHTML template.HTML
	Host        string
	Date        string
	SizeName    string
	Source      string
}

// SearchResults is the rendered outcome of a query.
type SearchResults struct {
	Query        string
	Global       bool
	TotalResults int
	Results      []SearchResult
	Failures     []string
	// Hint carries browse guidance shown when a filter-only query (a filetype:,
	// site:, tld: or inurl: operator with no keyword) matched nothing; empty
	// hides it.
	Hint string
}

// SearchSource runs an admin-console query against the node search core.
type SearchSource interface {
	Search(ctx context.Context, query SearchQuery) (SearchResults, error)
}
