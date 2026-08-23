package adminui

type crawlConnectionView struct {
	State  string
	Detail string
	Tag    string
}

func buildCrawlConnectionView(source CrawlerFetchActivitySource) crawlConnectionView {
	connectedCrawlers := -1
	if source != nil {
		connectedCrawlers = source.CrawlerFetchActivity().ConnectedCrawlers
	}

	switch {
	case connectedCrawlers < 0:
		return crawlConnectionView{
			State:  "Unavailable",
			Detail: "Connection telemetry is unavailable.",
			Tag:    "debug",
		}
	case connectedCrawlers == 0:
		return crawlConnectionView{
			State:  "Disconnected",
			Detail: "No crawlers are connected. Queued crawl orders remain pending until a crawler connects.",
			Tag:    "error",
		}
	default:
		return crawlConnectionView{
			State:  "Connected",
			Detail: crawlerCount(connectedCrawlers) + " connected.",
			Tag:    "success",
		}
	}
}
