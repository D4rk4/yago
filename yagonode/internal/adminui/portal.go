package adminui

import (
	"context"
	"log/slog"
	"net/http"
)

const portalPath = "/admin/portal"

// portalCategory is the settingCategory label whose runtime settings the Public
// portal section surfaces on its Configuration tab; the node tags portal-facing
// keys (portal.*, web.robots.*, public.base.url, https.redirect) with it.
const portalCategory = "Public portal"

type portalPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Settings   SettingsView
	Notice     string
	Error      string
}

func (c *Console) handlePortal(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		c.renderUnavailable(w, r, portalPath, "Public portal",
			"Runtime settings are not available on this deployment.")

		return
	}
	c.render(r.Context(), w, c.tpl.portal, "layout", c.portalPage(r, "", ""))
}

func (c *Console) handlePortalUpdate(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		http.NotFound(w, r)

		return
	}
	change := parseSettingsChange(r)
	if !portalKey(r.Context(), c.settings, change.Key) {
		http.NotFound(w, r)

		return
	}
	result, err := c.settings.Update(r.Context(), change)
	if err != nil {
		slog.WarnContext(r.Context(), "admin portal update failed", slog.Any("error", err))
	}
	notice, errMsg := settingsOutcome(result, err)
	c.render(r.Context(), w, c.tpl.portal, "layout", c.portalPage(r, notice, errMsg))
}

func (c *Console) portalPage(r *http.Request, notice, errMsg string) portalPageData {
	return portalPageData{
		AppName: appName, ActivePath: portalPath, Nav: navItems,
		CSRF:     csrfToken(r),
		Section:  sectionView{Heading: "Public portal", Available: true},
		Settings: portalSettings(r.Context(), c.settings),
		Notice:   notice,
		Error:    errMsg,
	}
}

// portalSettings filters the full runtime-settings catalog down to the
// portal-facing subset (settingCategory "Public portal"), preserving order.
func portalSettings(ctx context.Context, source SettingsSource) SettingsView {
	full := source.Settings(ctx)
	items := make([]SettingItem, 0, len(full.Items))
	for _, item := range full.Items {
		if item.Category == portalCategory {
			items = append(items, item)
		}
	}

	return SettingsView{Items: items}
}

// portalKey reports whether key names a portal-facing setting, gating the update
// handler to the Public-portal subset so a foreign key cannot be written here.
func portalKey(ctx context.Context, source SettingsSource, key string) bool {
	for _, item := range portalSettings(ctx, source).Items {
		if item.Key == key {
			return true
		}
	}

	return false
}
