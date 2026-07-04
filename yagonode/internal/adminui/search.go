package adminui

import "context"

// SearchQuery is an admin-console search request.
type SearchQuery struct {
	Query  string
	Global bool
}

// SearchResult is one rendered hit. Marked is set for DDGS web-fallback hits so
// this human surface can show the visible [ddgs] marker.
type SearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
	Host       string
	Date       string
	Source     string
	Marked     bool
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
