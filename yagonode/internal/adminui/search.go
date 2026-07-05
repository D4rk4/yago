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

// SearchResult is one rendered hit. Marked is set for DDGS web-fallback hits so
// this human surface can show the visible [ddgs] marker.
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
	Marked      bool
}

// SearchResults is the rendered outcome of a query.
type SearchResults struct {
	Query        string
	Global       bool
	TotalResults int
	Results      []SearchResult
	Failures     []string
}

// SearchSource runs an admin-console query against the node search core.
type SearchSource interface {
	Search(ctx context.Context, query SearchQuery) (SearchResults, error)
}
