package adminui

import (
	"net/url"
	"strconv"
)

const crawlRunsPerPage = 20

type CrawlRunPagination struct {
	Runs       []CrawlRunView
	Total      int
	Page       int
	Pages      int
	Start      int
	End        int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
	RefreshURL string
}

func buildCrawlRunPagination(runs []CrawlRunView, rawPage string) CrawlRunPagination {
	total := len(runs)
	pages := (total + crawlRunsPerPage - 1) / crawlRunsPerPage
	if pages < 1 {
		pages = 1
	}
	page := requestedCrawlRunPage(rawPage)
	if page > pages {
		page = pages
	}
	start := (page - 1) * crawlRunsPerPage
	end := start + crawlRunsPerPage
	if end > total {
		end = total
	}

	visibleRuns := make([]CrawlRunView, end-start)
	copy(visibleRuns, runs[start:end])
	pagination := CrawlRunPagination{
		Runs:       visibleRuns,
		Total:      total,
		Page:       page,
		Pages:      pages,
		HasPrev:    page > 1,
		HasNext:    page < pages,
		RefreshURL: crawlMonitorPageURL(page),
	}
	if total > 0 {
		pagination.Start = start + 1
		pagination.End = end
	}
	if pagination.HasPrev {
		pagination.PrevURL = crawlRunPageURL(page - 1)
	}
	if pagination.HasNext {
		pagination.NextURL = crawlRunPageURL(page + 1)
	}

	return pagination
}

func requestedCrawlRunPage(rawPage string) int {
	page, err := strconv.Atoi(rawPage)
	if err != nil || page < 1 {
		return 1
	}

	return page
}

func crawlRunPageURL(page int) string {
	return crawlPaginationURL(crawlPath, page) + "#crawl-monitor"
}

func crawlMonitorPageURL(page int) string {
	return crawlPaginationURL(crawlMonitorPath, page)
}

func crawlPaginationURL(path string, page int) string {
	values := url.Values{}
	if page > 1 {
		values.Set("cpage", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}

	return path
}
