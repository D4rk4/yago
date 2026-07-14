// Package websearch provides optional, admin-gated web retrieval after a local
// and federated miss or alongside those sources in always mode. Results are
// stamped with searchcore.SourceWeb so human search surfaces can label them with
// a "web" provenance badge while the Tavily-compatible API returns them
// unmarked.
package websearch

import "context"

// Result is one web-search hit as returned by a Provider.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Provider searches an external web-search backend. Implementations report
// operational failures to the fallback decorators, which preserve any primary
// answer and expose the provider as a partial failure.
type Provider interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}
