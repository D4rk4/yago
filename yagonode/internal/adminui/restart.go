package adminui

import (
	"fmt"
	"net/http"
)

const restartPath = "/admin/restart"

// restartPageData drives the restart confirmation page. CrawlerRestart shows the
// crawler-restart control; Notice reports the outcome of a crawler restart.
type restartPageData struct {
	AppName        string
	ActivePath     string
	Nav            []NavItem
	CSRF           string
	Section        sectionView
	CrawlerRestart bool
	Notice         string
}

func (c *Console) restartPageData(r *http.Request, notice string) restartPageData {
	return restartPageData{
		AppName: appName, ActivePath: restartPath, Nav: navItems,
		CSRF:           csrfToken(r),
		Section:        sectionView{Heading: "Restart", Available: true},
		CrawlerRestart: c.restartCrawlers != nil,
		Notice:         notice,
	}
}

// handleRestartPage renders the confirmation page: restarting the node drops
// in-flight crawls and searches for a few seconds, so the action is never one
// click. The crawler-restart control appears alongside it when a crawl fleet is
// reachable over the gRPC control plane.
func (c *Console) handleRestartPage(w http.ResponseWriter, r *http.Request) {
	if c.restart == nil && c.restartCrawlers == nil {
		c.renderUnavailable(w, r, restartPath, "Restart",
			"Restarting is not wired on this deployment.")

		return
	}
	c.render(r.Context(), w, c.tpl.restart, "layout", c.restartPageData(r, ""))
}

// handleRestartAction answers the confirmed form. A crawler target broadcasts a
// restart over the control plane and stays on the page with a notice; the node
// target renders the restarting notice first so the response reaches the browser
// (graceful shutdown waits for in-flight handlers), then fires the trigger.
func (c *Console) handleRestartAction(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("target") == "crawler" {
		c.handleCrawlerRestart(w, r)

		return
	}
	if c.restart == nil {
		http.NotFound(w, r)

		return
	}
	c.render(r.Context(), w, c.tpl.restart, "restarting-node", pageData{AppName: appName})
	c.restart()
}

func (c *Console) handleCrawlerRestart(w http.ResponseWriter, r *http.Request) {
	if c.restartCrawlers == nil {
		http.NotFound(w, r)

		return
	}
	notice := "No crawlers are connected."
	if signalled := c.restartCrawlers(); signalled > 0 {
		notice = fmt.Sprintf("Signalled %s to restart.", crawlerCount(signalled))
	}
	c.render(r.Context(), w, c.tpl.restart, "layout", c.restartPageData(r, notice))
}

// crawlerCount renders a worker count with a correctly pluralised noun.
func crawlerCount(n int) string {
	if n == 1 {
		return "1 crawler"
	}

	return fmt.Sprintf("%d crawlers", n)
}
