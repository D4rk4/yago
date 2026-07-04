package adminui

import "context"

// DocumentSummary is one indexed document as shown in the Index document browser.
type DocumentSummary struct {
	URL         string
	Title       string
	ContentType string
	Language    string
	FetchedAt   string
	IndexedAt   string
}

// DocumentQuery narrows the document browser by a URL substring and/or a domain
// (an exact host or any of its subdomains). Empty fields match everything.
type DocumentQuery struct {
	URLContains string
	Domain      string
}

// DocumentPage is a bounded slice of matching documents plus how many matched in
// total, so the browser can note when results were truncated.
type DocumentPage struct {
	Documents []DocumentSummary
	Matched   int
	Limit     int
	Truncated bool
}

// DocumentBrowserSource lists indexed documents matching a query, newest results
// first, bounded to a fixed page size. A nil provider hides the document browser.
type DocumentBrowserSource interface {
	BrowseDocuments(ctx context.Context, query DocumentQuery) DocumentPage
}
