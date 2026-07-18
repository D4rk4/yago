package pagefetch

import "context"

// browserEscalationKey marks a fetch context whose crawl profile disabled the
// headless-browser escalation. A context key inside this package — set by the
// pipeline, read only by FallbackPageSource — beats doubling the fetch-chain
// matrix a third time (TLS × robots × browser would be eight chains).
type browserEscalationKey struct{}

// WithoutBrowserFallback marks the fetch so a rejected fast fetch is not
// retried through the headless browser (profile DisableBrowser opt-out).
func WithoutBrowserFallback(ctx context.Context) context.Context {
	return context.WithValue(ctx, browserEscalationKey{}, true)
}

// browserFallbackDisabled reports whether the fetch opted out of browser
// escalation.
func browserFallbackDisabled(ctx context.Context) bool {
	disabled, _ := ctx.Value(browserEscalationKey{}).(bool)

	return disabled
}
