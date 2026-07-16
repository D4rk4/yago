package adminui

import (
	"net/http"
	"strings"
	"testing"
)

func TestPhotonStylesheetLinkedAndServed(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config: fakeConfig{view: ConfigView{}},
		Search: fakeSearch{},
		Settings: &fakeSettings{view: SettingsView{Items: []SettingItem{{
			Key: "network.name", Title: "Network name", Value: "freeworld", Category: "Network",
		}}}},
	})

	page := do(t, console, "/admin/configuration")
	if !strings.Contains(page.body, `href="`+mustAdminAssetReferences(assetFS)["photon.css"]+`"`) {
		t.Fatal("layout does not link the Photon stylesheet")
	}

	asset := do(t, console, "/admin/assets/photon.css")
	if asset.status != http.StatusOK {
		t.Fatalf("photon.css status %d", asset.status)
	}
	for _, want := range []string{"--ph-raise", ".cds-nav", "order: 2"} {
		if !strings.Contains(asset.body, want) {
			t.Fatalf("photon.css missing %q", want)
		}
	}
}

func TestNavIconsRenderFromLocalColorAssets(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := do(t, console, "/admin/configuration")
	references := mustAdminAssetReferences(assetFS)
	for _, item := range navItems {
		reference, found := references[item.Icon]
		if !found || !strings.Contains(got.body, `src="`+reference+`"`) {
			t.Fatalf("nav does not reference local icon %q", item.Icon)
		}
	}
	if !strings.Contains(got.body, `<img class="cds-nav__icon"`) ||
		strings.Contains(got.body, `<svg class="cds-nav__icon"`) {
		t.Fatal("nav icons missing their class")
	}
	asset := do(t, console, references[navItems[0].Icon])
	if asset.status != http.StatusOK || asset.header.Get("Content-Type") != "image/svg+xml" ||
		asset.header.Get("Cache-Control") != adminAssetImmutableCacheControl {
		t.Fatalf("local icon response = %d %v", asset.status, asset.header)
	}
}

func TestPhotonStylesEveryInteractiveControlState(t *testing.T) {
	t.Parallel()

	stylesheet, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, want := range []string{
		"--ph-well: #eff4ee",
		"--ph-selection: #8ea29b",
		"scrollbar-color: var(--ph-face) #a89f96",
		"*::-webkit-scrollbar-thumb",
		`.cds-btn:disabled`,
		`.cds-btn:focus-visible`,
		`select.cds-input {`,
		`appearance: none`,
		`.cds-input:user-invalid`,
		`.cds-input[type="number"]::-webkit-inner-spin-button`,
		`.cds-checkbox input[type="checkbox"]:checked`,
		`.cds-radio input[type="radio"]:checked`,
		`.cds-checkbox input:focus-visible`,
		`.cds-tabs.js-tabs .cds-tab::after`,
		`.cds-tabs.js-tabs .cds-tab:disabled`,
		`.cds-table tbody tr[aria-selected="true"]`,
		`.cds-ac-list li[aria-selected="true"]`,
		`.cds-pager a:focus-visible`,
		`.cds-restarting-page`,
		`.cds-restarting-card__bar`,
		"hr {",
		`@media (forced-colors: active)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("photon.css missing interactive state %q", want)
		}
	}
	if strings.Contains(css, "data:image") || strings.Contains(css, "http://") ||
		strings.Contains(css, "https://") {
		t.Fatal("Photon controls must not depend on inline or remote image assets")
	}
}

func TestPhotonPagesKeepNativeControlSemantics(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Config: fakeConfig{view: ConfigView{}},
		Search: fakeSearch{},
		Settings: &fakeSettings{view: SettingsView{Items: []SettingItem{{
			Key: "network.name", Title: "Network name", Value: "freeworld", Category: "Network",
		}}}},
	})
	search := do(t, console, "/admin/search").body
	for _, want := range []string{
		`<form class="cds-search-form"`,
		`<fieldset class="cds-field">`,
		`<legend class="cds-label">Scope</legend>`,
		`class="cds-radio"`,
		`type="radio" name="scope"`,
		`<button class="cds-btn" type="submit">Search</button>`,
	} {
		if !strings.Contains(search, want) {
			t.Errorf("search controls missing semantic markup %q", want)
		}
	}
	configuration := do(t, console, "/admin/configuration").body
	for _, want := range []string{
		`class="cds-tablist" role="tablist"`,
		`role="tab" class="cds-tab"`,
		`class="cds-tabpanel" role="tabpanel"`,
		`<fieldset class="cds-fieldset">`,
		`<legend class="cds-legend">`,
	} {
		if !strings.Contains(configuration, want) {
			t.Errorf("configuration controls missing semantic markup %q", want)
		}
	}
}

func TestPhotonSettingsRowsOwnContinuousSeparators(t *testing.T) {
	asset := do(t, New(Options{}), "/admin/assets/photon.css")
	for _, want := range []string{
		`.cds-settings-form .cds-setting-row {`,
		`display: grid;`,
		`grid-column: 1 / -1;`,
		`grid-template-columns: subgrid;`,
		`border-bottom: 1px solid var(--ph-shadow);`,
		`.cds-settings-form .cds-setting-row:last-of-type {`,
		`.cds-setting-row__reset {`,
		`padding: 2px var(--cds-spacing-03);`,
	} {
		if !strings.Contains(asset.body, want) {
			t.Fatalf("Photon settings layout missing %q", want)
		}
	}
	for _, gone := range []string{
		`.cds-settings-form .cds-setting-row { display: contents; }`,
		`.cds-setting-row:last-of-type .cds-setting-row__label`,
		`.cds-setting-row:last-of-type .cds-setting-row__control`,
	} {
		if strings.Contains(asset.body, gone) {
			t.Fatalf("Photon settings layout retains fragmented separator selector %q", gone)
		}
	}
}

func TestPhotonTabsFormContiguousScrollablePanelStrip(t *testing.T) {
	t.Parallel()

	stylesheet, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, want := range []string{
		`.cds-tabs.js-tabs .cds-tablist {`,
		`flex-wrap: nowrap;`,
		`gap: 0;`,
		`margin-bottom: 0;`,
		`overflow-x: auto;`,
		`scrollbar-width: thin;`,
		`.cds-tabs.js-tabs .cds-tablist::-webkit-scrollbar { height: 8px; }`,
		`height: var(--ph-ctl-h);`,
		`margin: 0 4px -1px 0;`,
		`width: 6px;`,
		`transform: skewX(14deg);`,
		`border-bottom-color: var(--cds-layer-02);`,
		`scroll-margin-top: calc(var(--cds-header-height) + var(--ph-ctl-h) + var(--cds-spacing-03));`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("Photon tab strip missing %q", want)
		}
	}
	for _, gone := range []string{
		`gap: 8px;`,
		`width: 12px;`,
		`transform: skewX(28deg);`,
		`font-weight: 700;\n  z-index: 1;`,
	} {
		if strings.Contains(css, gone) {
			t.Fatalf("Photon tab strip retains oversized geometry %q", gone)
		}
	}
}

func TestPhotonButtonsUseRaisedAndSunkenGeometry(t *testing.T) {
	t.Parallel()

	stylesheet, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, want := range []string{
		`border-radius: 0;`,
		`box-shadow: var(--ph-raise);`,
		`font-weight: 400;`,
		`.cds-btn:hover { background: #cccccc; }`,
		`.cds-btn:active { background: var(--ph-face); box-shadow: var(--ph-sink); transform: translate(1px, 1px); }`,
		`outline: 1px dotted var(--cds-text-primary);`,
		`.cds-btn:disabled,`,
		`.cds-btn--ghost { background: var(--ph-face); color: var(--cds-text-primary); }`,
		`.cds-btn--danger:active { background: #a84d47; box-shadow: var(--ph-sink); }`,
		`.cds-btn--danger:focus-visible { outline-color: #fff; }`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("Photon button states missing %q", want)
		}
	}
	if strings.Contains(
		css,
		`.cds-btn--ghost { background: var(--ph-face); color: var(--cds-interactive-active); }`,
	) {
		t.Fatal("ordinary Photon buttons retain link-blue text")
	}
}
