// Package websearch provides an optional, admin-gated web-search fallback that
// answers a query only after the local index and the federated peers return
// nothing. Fallback results are stamped with searchcore.SourceWeb so the human
// search surfaces can render a visible [ddgs] marker while the Tavily-compatible
// API returns them unmarked.
package websearch

import "context"

// Result is one web-search hit as returned by a Provider.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Provider searches an external web-search backend. Implementations must degrade
// to an empty result on rate limiting or backend failure rather than propagate a
// hard error that would fail the caller's search.
type Provider interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}
