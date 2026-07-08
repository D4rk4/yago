package adminui

import (
	"context"
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
	// DesignSearch and DesignResults feed the two design tabs' editors; nil
	// (no theme store wired) keeps the tabs as placeholders.
	DesignSearch  *designFormData
	DesignResults *designFormData
}

func (c *Console) handlePortal(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		c.renderUnavailable(w, r, portalPath, "Public portal",
			"Runtime settings are not available on this deployment.")

		return
	}
	c.renderPortalPage(w, r, "", "")
}

func (c *Console) handlePortalUpdate(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil {
		http.NotFound(w, r)

		return
	}
	notice, errMsg, ok := c.applySettingsBatch(r, c.portalGate(r.Context()))
	if !ok {
		http.NotFound(w, r)

		return
	}
	c.renderPortalPage(w, r, notice, errMsg)
}

// renderPortalPage renders the Public portal section with the page-scoped CSP
// the visual editor's canvas needs (ADR-0033).
func (c *Console) renderPortalPage(
	w http.ResponseWriter,
	r *http.Request,
	notice, errMsg string,
) {
	data := c.portalPage(r, notice, errMsg)
	policy := contentPol
	if c.theme != nil {
		search, results, err := c.portalDesignForms(r.Context(), data.CSRF)
		if err != nil && data.Error == "" {
			data.Error = "Loading the stored design failed: " + err.Error()
		}
		data.DesignSearch = search
		data.DesignResults = results
		policy = portalContentPol
	}
	c.renderPolicy(
		r.Context(),
		w,
		pageTemplate{tpl: c.tpl.portal, name: "layout"},
		data,
		policy,
	)
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

// withoutPortalCategory drops the portal-facing settings from the flat
// Configuration sheet: they are edited on the dedicated Public portal page, so
// showing the category twice would leave two competing forms for the same keys.
func withoutPortalCategory(items []SettingItem) []SettingItem {
	kept := make([]SettingItem, 0, len(items))
	for _, item := range items {
		if item.Category == portalCategory {
			continue
		}
		kept = append(kept, item)
	}

	return kept
}

// portalGate precomputes the portal-facing key whitelist so the batch can gate
// each submitted key without re-reading the settings catalog per key, keeping a
// foreign key from being written through this page.
func (c *Console) portalGate(ctx context.Context) settingsGate {
	allowed := map[string]bool{}
	for _, item := range portalSettings(ctx, c.settings).Items {
		allowed[item.Key] = true
	}

	return func(key string) bool { return allowed[key] }
}
