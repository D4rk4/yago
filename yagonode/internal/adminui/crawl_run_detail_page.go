package adminui

import (
	"net/http"
	"strings"
)

func (c *Console) handleCrawlRunDetail(w http.ResponseWriter, r *http.Request) {
	if c.crawlRunDetails == nil {
		http.NotFound(w, r)

		return
	}
	data := crawlRunPageData{
		AppName: appName, ActivePath: crawlPath, Nav: navItems,
		CSRF: csrfToken(r), Section: sectionView{Heading: "Crawl run", Available: true},
	}
	runID := strings.TrimSpace(r.URL.Query().Get("runId"))
	if runID == "" {
		data.Error = "Select a crawl run."
	} else {
		detail, err := c.crawlRunDetails.CrawlRunDetail(r.Context(), runID)
		if err != nil {
			data.Error = "Crawl run detail is unavailable: " + err.Error()
		} else {
			data.Detail = detail
		}
	}
	c.render(r.Context(), w, c.tpl.crawlRun, "layout", data)
}
