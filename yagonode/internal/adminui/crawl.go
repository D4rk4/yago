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
	// NoindexCanonicalMismatch skips indexing pages whose rel=canonical points
	// at a different URL while still following their links.
	NoindexCanonicalMismatch bool
	IgnoreTLSAuthority       bool
	IgnoreRobots             bool
	DisableBrowser           bool
	RecrawlIfOlder           string
	CrawlDelay               string
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

// FormatSettings mirrors the shared document-format toggles every crawl uses
// (YaCy TextParser families). HTML/web text is always on and carries no toggle.
type FormatSettings struct {
	Text     bool
	XMLFeeds bool
	PDF      bool
	Office   bool
	Images   bool
	Audio    bool
	Misc     bool
	Archives bool
}

// CrawlFormatsSource reads and saves the shared format toggles. A nil source
// hides the formats block.
type CrawlFormatsSource interface {
	CurrentFormats(ctx context.Context) FormatSettings
	SaveFormats(ctx context.Context, settings FormatSettings) error
}

// CrawlScheduleView is one recurring crawl as listed in the console (UI-19,
// YaCy Automation_p parity).
type CrawlScheduleView struct {
	ID       string
	Name     string
	Seeds    int
	Scope    string
	Interval string
	LastRun  string
	Enabled  bool
}

// CrawlScheduleRequest creates one recurring crawl.
type CrawlScheduleRequest struct {
	Name     string
	Seeds    []string
	Scope    string
	MaxDepth int
	// Interval is a Go duration string; the store enforces the hour floor.
	Interval string
}

// CrawlScheduleSource manages recurring crawls. A nil provider hides the
// schedules block.
type CrawlScheduleSource interface {
	Schedules(ctx context.Context) []CrawlScheduleView
	CreateSchedule(ctx context.Context, req CrawlScheduleRequest) error
	DeleteSchedule(ctx context.Context, id string) error
	SetScheduleEnabled(ctx context.Context, id string, enabled bool) error
}
