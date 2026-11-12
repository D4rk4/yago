package adminui

import "net/http"

const (
	crawlMonitorLiveTarget = "crawl-live"
	crawlHistoryListTarget = "crawl-history-list"
)

func (c *Console) renderCrawlMonitor(w http.ResponseWriter, r *http.Request) {
	templateName := "crawl-monitor"
	switch r.Header.Get("HX-Target") {
	case crawlMonitorLiveTarget:
		templateName = "crawl-monitor-live"
	case crawlHistoryListTarget:
		templateName = "crawl-history-list"
	}
	c.render(r.Context(), w, c.tpl.crawl, templateName, c.crawlMonitorView(r))
}
