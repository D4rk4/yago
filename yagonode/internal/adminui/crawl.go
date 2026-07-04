package adminui

import "context"

// CrawlStart is an operator's crawl-start request as entered in the console form.
// The expert fields mirror the crawl profile: the four regex filters and the
// duration fields are passed through as text for the dispatcher to compile and
// validate, so a bad regex surfaces as a start error rather than being silently
// dropped. An empty RecrawlIfOlder or CrawlDelay leaves the field at its default.
type CrawlStart struct {
	Name                 string
	Seeds                []string
	Mode                 string
	Scope                string
	MaxDepth             int
	URLMustMatch         string
	URLMustNotMatch      string
	IndexURLMustMatch    string
	IndexURLMustNotMatch string
	MaxPagesPerHost      int
	AllowQueryURLs       bool
	FollowNoFollowLinks  bool
	RecrawlIfOlder       string
	CrawlDelay           string
}

// CrawlDispatch is the outcome of a crawl the console accepted for dispatch.
type CrawlDispatch struct {
	ProfileHandle string
	Seeds         int
	Duplicate     bool
}

// CrawlSource dispatches operator crawl requests for the console's Crawler
// section. A nil provider renders the section in a controlled unavailable state.
type CrawlSource interface {
	Start(ctx context.Context, start CrawlStart) (CrawlDispatch, error)
}
