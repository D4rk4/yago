package adminui

import "fmt"

type CrawlerFetchActivity struct {
	ConnectedCrawlers      int
	ActiveFetches          int
	FetchLimitPerCrawler   int
	AggregateFetchCapacity int
	ActiveFetchesKnown     bool
}

type CrawlerFetchActivitySource interface {
	CrawlerFetchActivity() CrawlerFetchActivity
}

func applyCrawlerFetchActivity(
	view systemMonitorView,
	source CrawlerFetchActivitySource,
) systemMonitorView {
	if source == nil {
		return view
	}
	activity := source.CrawlerFetchActivity()
	if activity.ConnectedCrawlers <= 0 {
		return view
	}
	view.CrawlerFetchVisible = true
	if !validCrawlerFetchActivity(activity) {
		return view
	}

	view.CrawlerFetchAvailable = true
	view.CrawlerFetchValue = min(activity.ActiveFetches, activity.AggregateFetchCapacity)
	view.CrawlerFetchMaximum = activity.AggregateFetchCapacity
	view.CrawlerFetchText = fmt.Sprintf(
		"%d active of %d",
		activity.ActiveFetches,
		activity.AggregateFetchCapacity,
	)
	if activity.ConnectedCrawlers > 1 {
		view.CrawlerFetchText += fmt.Sprintf(
			" · %d crawlers × %d each",
			activity.ConnectedCrawlers,
			activity.FetchLimitPerCrawler,
		)
	}

	return view
}

func validCrawlerFetchActivity(activity CrawlerFetchActivity) bool {
	if !activity.ActiveFetchesKnown || activity.ConnectedCrawlers <= 0 ||
		activity.ActiveFetches < 0 || activity.FetchLimitPerCrawler <= 0 {
		return false
	}
	connected := int64(activity.ConnectedCrawlers)
	perCrawler := int64(activity.FetchLimitPerCrawler)
	capacity := int64(activity.AggregateFetchCapacity)

	return capacity/perCrawler == connected && capacity%perCrawler == 0
}
