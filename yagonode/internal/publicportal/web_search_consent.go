package publicportal

import "context"

type WebSearchConsentSource interface {
	SearchWithWebConsent(
		ctx context.Context,
		query, dom string,
		offset, limit int,
		consented bool,
	) (SearchResults, error)
}

func searchWithWebConsent(
	ctx context.Context,
	source SearchSource,
	query, dom string,
	offset, limit int,
	consented bool,
) (SearchResults, error) {
	if consentSource, ok := source.(WebSearchConsentSource); ok {
		return consentSource.SearchWithWebConsent(ctx, query, dom, offset, limit, consented)
	}

	return source.Search(ctx, query, dom, offset, limit)
}
