package adminui

import "context"

type CrawlURLOutcomeView struct {
	URL        string
	Class      string
	ObservedAt string
	HTTPStatus string
	Reason     string
}

type CrawlRunDetail struct {
	Run      CrawlRunView
	Outcomes []CrawlURLOutcomeView
}

type CrawlRunDetailSource interface {
	CrawlRunDetail(context.Context, string) (CrawlRunDetail, error)
}
