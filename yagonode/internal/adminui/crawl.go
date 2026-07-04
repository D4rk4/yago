package adminui

import "context"

// CrawlStart is an operator's crawl-start request as entered in the console form.
type CrawlStart struct {
	Name     string
	Seeds    []string
	Mode     string
	Scope    string
	MaxDepth int
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
