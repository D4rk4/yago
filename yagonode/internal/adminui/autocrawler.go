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
	"swarm.seed.enabled":                true,
	"swarm.seed.limit":                  true,
	"swarm.seed.depth":                  true,
	"swarm.seed.max_pages":              true,
	"web.fallback.seed_crawl":           true,
	"web.fallback.seed_depth":           true,
	"web.fallback.seed_max_pages":       true,
	"autocrawler.crawl.query_urls":      true,
	"autocrawler.crawl.tls_insecure":    true,
	"autocrawler.crawl.ignore_robots":   true,
	"autocrawler.crawl.no_browser":      true,
	"autocrawler.crawl.follow_nofollow": true,
}

type autocrawlerPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Settings   SettingsView
	// Formats carries the shared document-format toggles; nil hides the block.
	Formats *FormatSettings
	// FormatsNote flashes the outcome of a formats save.
	FormatsNote string
	Notice      string
	Error       string
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
	data := autocrawlerPageData{
		AppName: appName, ActivePath: autocrawlerPath, Nav: navItems,
		CSRF:     csrfToken(r),
		Section:  sectionView{Heading: "Autocrawler", Available: true},
		Settings: autocrawlerSettings(r.Context(), c.settings),
		Notice:   notice,
		Error:    errMsg,
	}
	if c.crawlFormats != nil {
		settings := c.crawlFormats.CurrentFormats(r.Context())
		data.Formats = &settings
	}

	return data
}

// handleAutocrawlerFormats saves the shared document-format toggles from the
// autocrawler page, reusing the same source the manual crawler writes so both
// surfaces edit one shared setting.
func (c *Console) handleAutocrawlerFormats(w http.ResponseWriter, r *http.Request) {
	if c.crawlFormats == nil {
		http.NotFound(w, r)

		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}
	settings := FormatSettings{
		Text:     r.PostForm.Get("text") == "on",
		XMLFeeds: r.PostForm.Get("xmlfeeds") == "on",
		PDF:      r.PostForm.Get("pdf") == "on",
		Office:   r.PostForm.Get("office") == "on",
		Images:   r.PostForm.Get("images") == "on",
		Audio:    r.PostForm.Get("audio") == "on",
		Misc:     r.PostForm.Get("misc") == "on",
		Archives: r.PostForm.Get("archives") == "on",
	}
	if err := c.crawlFormats.SaveFormats(r.Context(), settings); err != nil {
		slog.WarnContext(r.Context(), "save autocrawler formats failed", slog.Any("error", err))
		data := c.autocrawlerPage(r, "", "")
		data.FormatsNote = "Saving format settings failed."
		c.render(r.Context(), w, c.tpl.autocrawler, "layout", data)

		return
	}
	http.Redirect(w, r, autocrawlerPath, http.StatusSeeOther)
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
