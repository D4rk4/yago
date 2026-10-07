package adminui

import (
	"fmt"
	"net/http"
)

const (
	crawlOrderClearPath         = "/admin/crawl/queue/clear"
	crawlOrderClearConfirmation = "CLEAR PENDING ORDERS"
)

type crawlOrderClearPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	QueueKnown bool
	CanClear   bool
	Pending    int
	Leased     int
	Error      string
}

func (c *Console) handleCrawlOrderClearConfirmation(
	w http.ResponseWriter,
	r *http.Request,
) {
	if c.crawl == nil || c.crawlOrderClearSource == nil {
		http.NotFound(w, r)

		return
	}
	c.render(r.Context(), w, c.tpl.crawlOrderClear, "layout", c.crawlOrderClearPage(r, ""))
}

func (c *Console) handleCrawlOrderClear(w http.ResponseWriter, r *http.Request) {
	if c.crawl == nil || c.crawlOrderClearSource == nil {
		http.NotFound(w, r)

		return
	}
	if r.PostFormValue("confirmation") != crawlOrderClearConfirmation {
		c.render(r.Context(), w, c.tpl.crawlOrderClear, "layout",
			c.crawlOrderClearPage(r, "Confirmation did not match. No orders were cleared."))

		return
	}
	if c.monitor == nil {
		c.renderCrawlerWithClearError(
			w,
			r,
			"Pending order counts are unavailable; no orders were cleared.",
		)

		return
	}
	monitor := c.monitor.Monitor(r.Context())
	if !monitor.QueueAvailable {
		c.renderCrawlerWithClearError(
			w,
			r,
			"Pending order counts are unavailable; no orders were cleared.",
		)

		return
	}
	if monitor.QueuePending == 0 {
		data := c.crawlPage(r, defaultCrawlForm())
		data.Section.Message = "No pending orders were reported; no clear was started."
		c.render(r.Context(), w, c.tpl.crawl, "layout", data)

		return
	}
	removed, err := c.crawlOrderClearSource.ClearPendingOrders(r.Context())
	data := c.crawlPage(r, defaultCrawlForm())
	switch {
	case err != nil && removed == 0:
		data.Error = "Pending orders could not be cleared; no orders were removed."
	case err != nil:
		data.Error = fmt.Sprintf(
			"Pending orders removed: %d. The clear did not finish; some pending orders may remain.",
			removed,
		)
	case removed == 0:
		data.Section.Message = "No pending orders were removed."
	default:
		data.Section.Message = fmt.Sprintf(
			"Pending orders removed: %d. Leased orders and active fetches were left alone. "+
				"Orders accepted after the deletion boundary is set remain queued.",
			removed,
		)
	}
	c.render(r.Context(), w, c.tpl.crawl, "layout", data)
}

func (c *Console) crawlOrderClearPage(r *http.Request, message string) crawlOrderClearPageData {
	data := crawlOrderClearPageData{
		AppName:    appName,
		ActivePath: crawlPath,
		Nav:        navItems,
		CSRF:       csrfToken(r),
		Section:    sectionView{Heading: "Clear pending orders", Available: true},
		Error:      message,
	}
	if c.monitor == nil {
		return data
	}
	monitor := c.monitor.Monitor(r.Context())
	data.QueueKnown = monitor.QueueAvailable
	data.Pending = monitor.QueuePending
	data.Leased = monitor.QueueLeased
	data.CanClear = monitor.QueueAvailable && monitor.QueuePending > 0

	return data
}

func (c *Console) renderCrawlerWithClearError(
	w http.ResponseWriter,
	r *http.Request,
	message string,
) {
	data := c.crawlPage(r, defaultCrawlForm())
	data.Error = message
	c.render(r.Context(), w, c.tpl.crawl, "layout", data)
}
