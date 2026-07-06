package adminui

import "net/http"

const restartPath = "/admin/restart"

// handleRestartPage renders the confirmation page: restarting drops in-flight
// crawls and searches for a few seconds, so the action is never one click.
func (c *Console) handleRestartPage(w http.ResponseWriter, r *http.Request) {
	if c.restart == nil {
		c.renderUnavailable(w, r, restartPath, "Restart node",
			"Restarting is not wired on this deployment.")

		return
	}
	c.render(r.Context(), w, c.tpl.restart, "layout", pageData{
		AppName: appName, ActivePath: restartPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Restart node", Available: true},
	})
}

// handleRestartAction answers the confirmed form: the restarting notice is
// rendered first so the response reaches the browser (graceful shutdown waits
// for in-flight handlers), then the restart trigger fires.
func (c *Console) handleRestartAction(w http.ResponseWriter, r *http.Request) {
	if c.restart == nil {
		http.NotFound(w, r)

		return
	}
	c.render(r.Context(), w, c.tpl.restart, "restarting-node", pageData{AppName: appName})
	c.restart()
}
