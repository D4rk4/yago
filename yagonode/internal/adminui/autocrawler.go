package adminui

import (
	"context"
	"log/slog"
	"net/http"
)

const autocrawlerPath = "/admin/autocrawler"

// autocrawlerKeys is the runtime-settings subset the Autocrawler section
// edits: the two discovery paths that grow the index without a manual crawl —
// swarm greedy learning and web-fallback seeding — plus their crawl profiles.
var autocrawlerKeys = map[string]bool{
	"swarm.seed.enabled":          true,
	"swarm.seed.limit":            true,
	"swarm.seed.depth":            true,
	"swarm.seed.max_pages":        true,
	"web.fallback.seed_crawl":     true,
	"web.fallback.seed_depth":     true,
	"web.fallback.seed_max_pages": true,
}

type autocrawlerPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Settings   SettingsView
	Notice     string
	Error      string
}

func (c *Console) handleAutocrawler(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		c.renderUnavailable(w, r, autocrawlerPath, "Autocrawler",
			"Runtime settings are not available on this deployment.")

		return
	}
	c.render(r.Context(), w, c.tpl.autocrawler, "layout", c.autocrawlerPage(r, "", ""))
}

func (c *Console) handleAutocrawlerUpdate(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		http.NotFound(w, r)

		return
	}
	change := parseSettingsChange(r)
	if !autocrawlerKeys[change.Key] {
		http.NotFound(w, r)

		return
	}
	result, err := c.settings.Update(r.Context(), change)
	if err != nil {
		slog.WarnContext(r.Context(), "admin autocrawler update failed", slog.Any("error", err))
	}
	notice, errMsg := settingsOutcome(result, err)
	c.render(r.Context(), w, c.tpl.autocrawler, "layout", c.autocrawlerPage(r, notice, errMsg))
}

func (c *Console) autocrawlerPage(r *http.Request, notice, errMsg string) autocrawlerPageData {
	return autocrawlerPageData{
		AppName: appName, ActivePath: autocrawlerPath, Nav: navItems,
		CSRF:     csrfToken(r),
		Section:  sectionView{Heading: "Autocrawler", Available: true},
		Settings: autocrawlerSettings(r.Context(), c.settings),
		Notice:   notice,
		Error:    errMsg,
	}
}

// autocrawlerSettings filters the full runtime-settings catalog down to the
// autocrawler subset, preserving the catalog's order.
func autocrawlerSettings(ctx context.Context, source SettingsSource) SettingsView {
	full := source.Settings(ctx)
	items := make([]SettingItem, 0, len(autocrawlerKeys))
	for _, item := range full.Items {
		if autocrawlerKeys[item.Key] {
			items = append(items, item)
		}
	}

	return SettingsView{Items: items}
}
