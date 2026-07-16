package adminui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// fakeThemeStore is a programmable ThemeStore double: documents live in a map
// and each operation can be forced to fail.
type fakeThemeStore struct {
	docs    map[string]ThemeDocument
	enabled bool

	failDocument string
	failSave     string
	failReset    string
	failEnable   bool

	savedPages  []string
	resetPages  []string
	setEnabled  []bool
	savedBodies map[string]string
}

func newFakeThemeStore() *fakeThemeStore {
	return &fakeThemeStore{
		docs:        map[string]ThemeDocument{},
		savedBodies: map[string]string{},
	}
}

var errThemeStore = errors.New("theme store failure")

func (f *fakeThemeStore) Enabled() bool { return f.enabled }

func (f *fakeThemeStore) SetEnabled(_ context.Context, enabled bool) error {
	if f.failEnable {
		return errThemeStore
	}
	f.enabled = enabled
	f.setEnabled = append(f.setEnabled, enabled)

	return nil
}

func (f *fakeThemeStore) Document(_ context.Context, page string) (ThemeDocument, bool, error) {
	if f.failDocument == page {
		return ThemeDocument{}, false, errThemeStore
	}
	doc, ok := f.docs[page]

	return doc, ok, nil
}

func (f *fakeThemeStore) SaveDocument(
	_ context.Context,
	page, body string,
) (ThemeDocument, error) {
	if f.failSave == page {
		return ThemeDocument{}, errThemeStore
	}
	doc := ThemeDocument{Body: body, ParseOK: !strings.Contains(body, "{{broken")}
	if !doc.ParseOK {
		doc.ParseError = "Parse error on line 1."
	}
	f.docs[page] = doc
	f.savedPages = append(f.savedPages, page)
	f.savedBodies[page] = body

	return doc, nil
}

func (f *fakeThemeStore) ResetDocument(_ context.Context, page string) (bool, error) {
	if f.failReset == page {
		return false, errThemeStore
	}
	_, existed := f.docs[page]
	delete(f.docs, page)
	f.resetPages = append(f.resetPages, page)

	return existed, nil
}

func (f *fakeThemeStore) DefaultBody(page string) string {
	return "default body of " + page
}

func themedConsole(store ThemeStore) *Console {
	return New(Options{Settings: portalTestSettings(), Theme: store})
}

func TestPortalDesignTabsRenderEditors(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	store.docs["results"] = ThemeDocument{Body: "<p>custom results</p>", ParseOK: true}
	store.docs["styles"] = ThemeDocument{Body: "body { color: teal; }", ParseOK: true}
	store.enabled = true
	got := do(t, themedConsole(store), "/admin/portal")

	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		`action="/admin/portal/design"`,
		`data-designer="search"`,
		`data-designer="results"`,
		"default body of search",
		"&lt;p&gt;custom results&lt;/p&gt;",
		"body { color: teal; }",
		`name="enabled" value="true" checked`,
		`>Save design</button>`,
		"/admin/assets/vendor/grapes.min.js",
		"/admin/assets/vendor/codemirror.min.js",
		"/admin/assets/vendor/cm-simple.min.js",
		mustAdminAssetReferences(assetFS)["portal_designer.js"],
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("design tabs missing %q", want)
		}
	}
	simple := strings.Index(got.body, "/cm-simple.min.js")
	handlebars := strings.Index(got.body, "/cm-handlebars.min.js")
	if simple < 0 || handlebars < 0 || simple > handlebars {
		t.Error("CodeMirror simple mode must load before Handlebars mode")
	}
	if got.header.Get("Content-Security-Policy") != portalContentPol {
		t.Errorf("portal page CSP = %q, want the editor policy",
			got.header.Get("Content-Security-Policy"))
	}
	if strings.Count(got.body, `value="default">Default design</button>`) != 1 {
		t.Error("only the overridden results tab may offer Default design")
	}
	if strings.Count(got.body, `value="default-styles">Default styles</button>`) != 2 {
		t.Error("both tabs must offer Default styles while styles are customized")
	}
}

func TestPortalDesignEscapesTemplateBody(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	store.docs["search"] = ThemeDocument{
		Body:    `</textarea><script>alert(1)</script>{{query}}`,
		ParseOK: true,
	}
	got := do(t, themedConsole(store), "/admin/portal")

	if strings.Contains(got.body, "</textarea><script>alert(1)") {
		t.Fatal("template body must not escape its textarea")
	}
	if !strings.Contains(got.body, "&lt;/textarea&gt;&lt;script&gt;") {
		t.Fatal("template body must render escaped")
	}
}

func TestPortalDesignShowsParseFailure(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	store.docs["search"] = ThemeDocument{
		Body:       "{{broken",
		ParseOK:    false,
		ParseError: "Parse error on line 1.",
	}
	got := do(t, themedConsole(store), "/admin/portal")

	if !strings.Contains(got.body, "does not parse: Parse error on line 1.") {
		t.Error("the stored parse failure must be surfaced on the editor")
	}
}

func TestPortalDesignSurfacesLoadFailure(t *testing.T) {
	t.Parallel()

	for _, failing := range []string{"styles", "search", "results"} {
		t.Run(failing, func(t *testing.T) {
			t.Parallel()
			store := newFakeThemeStore()
			store.failDocument = failing
			got := do(t, themedConsole(store), "/admin/portal")

			if !strings.Contains(got.body, "Loading the stored design failed") {
				t.Error("a store read failure must be surfaced")
			}
			if strings.Contains(got.body, "design store is not available") ||
				!strings.Contains(got.body, "editor could not be loaded") {
				t.Fatalf("read failure misreported as an unwired store: %s", got.body)
			}
		})
	}
}

func TestPortalDesignSaveStoresTemplateStylesAndToggle(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	console := themedConsole(store)
	posted := doPost(t, console, "/admin/portal/design", url.Values{
		"page":    {"search"},
		"body":    {"<html>{{query}}</html>"},
		"styles":  {"body { margin: 0; }"},
		"enabled": {"true"},
	})

	if posted.status != http.StatusOK || !strings.Contains(posted.body, "Design saved.") {
		t.Fatalf("save = %d %.80q", posted.status, posted.body)
	}
	if store.savedBodies["search"] != "<html>{{query}}</html>" {
		t.Errorf("template not stored: %+v", store.savedBodies)
	}
	if store.savedBodies["styles"] != "body { margin: 0; }" {
		t.Errorf("styles not stored: %+v", store.savedBodies)
	}
	if len(store.setEnabled) != 1 || store.setEnabled[0] != true {
		t.Errorf("toggle not applied: %+v", store.setEnabled)
	}
}

func TestPortalDesignSaveUncheckedDisablesTheme(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	store.enabled = true
	doPost(t, themedConsole(store), "/admin/portal/design", url.Values{
		"page": {"results"}, "body": {"<p>r</p>"}, "styles": {""},
	})

	if len(store.setEnabled) != 1 || store.setEnabled[0] != false {
		t.Fatalf("an unchecked box must disable the theme: %+v", store.setEnabled)
	}
}

func TestPortalDesignSaveReportsParseFailure(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	posted := doPost(t, themedConsole(store), "/admin/portal/design", url.Values{
		"page": {"search"}, "body": {"{{broken"}, "styles": {""},
	})

	if !strings.Contains(posted.body, "Saved, but the template does not parse") {
		t.Fatalf("parse failure must be reported: %.120q", posted.body)
	}
	if store.savedBodies["search"] != "{{broken" {
		t.Error("an unparseable body must still be stored")
	}
}

func TestPortalDesignDefaultButtonsReset(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	store.docs["search"] = ThemeDocument{Body: "custom", ParseOK: true}
	store.docs["styles"] = ThemeDocument{Body: "custom css", ParseOK: true}
	console := themedConsole(store)

	posted := doPost(t, console, "/admin/portal/design", url.Values{
		"page": {"search"}, "action": {"default"},
	})
	if !strings.Contains(posted.body, "Default design restored.") {
		t.Fatalf("default design notice missing: %.80q", posted.body)
	}
	if len(store.resetPages) != 1 || store.resetPages[0] != "search" {
		t.Fatalf("reset pages = %+v", store.resetPages)
	}

	posted = doPost(t, console, "/admin/portal/design", url.Values{
		"page": {"results"}, "action": {"default-styles"},
	})
	if !strings.Contains(posted.body, "Default styles restored.") {
		t.Fatalf("default styles notice missing: %.80q", posted.body)
	}
	if len(store.resetPages) != 2 || store.resetPages[1] != "styles" {
		t.Fatalf("reset pages = %+v", store.resetPages)
	}
}

func TestPortalDesignRejectsUnknownPageAndMissingStore(t *testing.T) {
	t.Parallel()

	store := newFakeThemeStore()
	posted := doPost(t, themedConsole(store), "/admin/portal/design", url.Values{
		"page": {"styles"}, "body": {"x"},
	})
	if posted.status != http.StatusNotFound {
		t.Fatalf("styles is not a page target: %d, want 404", posted.status)
	}
	if len(store.savedPages) != 0 {
		t.Fatalf("nothing may be written: %+v", store.savedPages)
	}

	noTheme := New(Options{Settings: portalTestSettings()})
	posted = doPost(t, noTheme, "/admin/portal/design", url.Values{
		"page": {"search"}, "body": {"x"},
	})
	if posted.status != http.StatusNotFound {
		t.Fatalf("no theme store = %d, want 404", posted.status)
	}

	noSettings := New(Options{Theme: newFakeThemeStore()})
	posted = doPost(t, noSettings, "/admin/portal/design", url.Values{
		"page": {"search"}, "body": {"x"},
	})
	if posted.status != http.StatusNotFound {
		t.Fatalf("no settings = %d, want 404", posted.status)
	}
}

func TestPortalDesignSurfacesStoreFailures(t *testing.T) {
	t.Parallel()

	for name, prepare := range map[string]struct {
		arm  func(store *fakeThemeStore)
		form url.Values
		want string
	}{
		"template save": {
			arm:  func(store *fakeThemeStore) { store.failSave = "search" },
			form: url.Values{"page": {"search"}, "body": {"x"}},
			want: "Saving the design failed",
		},
		"styles save": {
			arm:  func(store *fakeThemeStore) { store.failSave = "styles" },
			form: url.Values{"page": {"search"}, "body": {"x"}},
			want: "Saving the shared styles failed",
		},
		"toggle": {
			arm:  func(store *fakeThemeStore) { store.failEnable = true },
			form: url.Values{"page": {"search"}, "body": {"x"}},
			want: "Switching the theme failed",
		},
		"default design": {
			arm:  func(store *fakeThemeStore) { store.failReset = "search" },
			form: url.Values{"page": {"search"}, "action": {"default"}},
			want: "Restoring the default design failed",
		},
		"default styles": {
			arm:  func(store *fakeThemeStore) { store.failReset = "styles" },
			form: url.Values{"page": {"search"}, "action": {"default-styles"}},
			want: "Restoring the default styles failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newFakeThemeStore()
			prepare.arm(store)
			posted := doPost(t, themedConsole(store), "/admin/portal/design", prepare.form)
			if !strings.Contains(posted.body, prepare.want) {
				t.Fatalf("missing %q in %.120q", prepare.want, posted.body)
			}
		})
	}
}

// TestPortalDesignVendorAssetsServed pins the vendored editor assets: each file
// is embedded, served from origin with the shared cache and sniff headers, and
// keeps its license header, per the ADR-0033 dependency rule for bundled assets.
func TestPortalDesignVendorAssetsServed(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	for _, path := range []string{
		"/admin/assets/vendor/grapes.min.js",
		"/admin/assets/vendor/grapes.min.css",
		"/admin/assets/vendor/grapesjs-preset-webpage.min.js",
		"/admin/assets/vendor/codemirror.min.js",
		"/admin/assets/vendor/codemirror.min.css",
		"/admin/assets/vendor/cm-xml.min.js",
		"/admin/assets/vendor/cm-javascript.min.js",
		"/admin/assets/vendor/cm-css.min.js",
		"/admin/assets/vendor/cm-htmlmixed.min.js",
		"/admin/assets/vendor/cm-handlebars.min.js",
		"/admin/assets/vendor/cm-multiplex.min.js",
		"/admin/assets/vendor/cm-simple.min.js",
		"/admin/assets/vendor/font-awesome.min.css",
		"/admin/assets/fonts/fontawesome-webfont.woff2",
		"/admin/assets/portal_designer.js",
		"/admin/assets/portal_designer.css",
	} {
		got := do(t, console, path)
		if got.status != http.StatusOK || len(got.body) == 0 {
			t.Errorf("%s: status %d, %d bytes", path, got.status, len(got.body))

			continue
		}
		assertPortalDesignerAssetHeaders(t, path, got)
		assertPortalDesignerVendorLicense(t, path, got)
		assertPortalDesignerIconFont(t, path, got)
		assertPortalDesignerBootstrap(t, path, got)
	}
}

func assertPortalDesignerAssetHeaders(t *testing.T, path string, got capture) {
	t.Helper()
	if got.header.Get("X-Content-Type-Options") != "nosniff" ||
		got.header.Get("Cache-Control") == "" {
		t.Errorf("%s: asset headers missing", path)
	}
}

func assertPortalDesignerVendorLicense(t *testing.T, path string, got capture) {
	t.Helper()
	if strings.Contains(path, "/vendor/") && !strings.HasPrefix(got.body, "/*!") {
		t.Errorf("%s: vendored asset lost its license header", path)
	}
}

func assertPortalDesignerIconFont(t *testing.T, path string, got capture) {
	t.Helper()
	if strings.HasSuffix(path, "/font-awesome.min.css") &&
		(!strings.Contains(got.body, "url('../fonts/fontawesome-webfont.woff2?v=4.7.0')") ||
			strings.Contains(got.body, "url('http")) {
		t.Errorf("%s: icon font is not pinned to the local WOFF2 asset", path)
	}
	if strings.HasSuffix(path, "/fontawesome-webfont.woff2") {
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(got.body)))
		if sum != "2adefcbc041e7d18fcf2d417879dc5a09997aa64d675b7a3c4b6ce33da13f3fe" {
			t.Errorf("%s: icon font content changed: %s", path, sum)
		}
	}
}

func assertPortalDesignerBootstrap(t *testing.T, path string, got capture) {
	t.Helper()
	if strings.HasSuffix(path, "/portal_designer.js") {
		for _, want := range []string{
			`root.matches("form")`,
			`canvas: { frameStyle: styleParts.frame }`,
			`protectedCss: ""`,
			`style: styleParts.visual`,
			`avoidProtected: true, keepUnusedStyles: true`,
			`webpagePlugin(editor, { useCustomTheme: false })`,
			`cssIcons: "/admin/assets/vendor/font-awesome.min.css?v=4.7.0"`,
			`preserveScripts(tplCM.getValue())`,
			`docParts = splitDocument(preservedScripts.document)`,
			`data-yago-preserved-script=`,
			`docParts.prefix + visualComponents(grapes) + docParts.suffix`,
			`document.replace(marker, function (placeholder, token, rawIndex)`,
			`grapes.destroy()`,
		} {
			if !strings.Contains(got.body, want) {
				t.Errorf("%s: editor bootstrap missing %q", path, want)
			}
		}
		for _, stale := range []string{
			`style: cssCM.getValue()`,
			`setStyle(cssCM.getValue())`,
		} {
			if strings.Contains(got.body, stale) {
				t.Errorf("%s: editor restored stale full-stylesheet import %q", path, stale)
			}
		}
	}
	if strings.HasSuffix(path, "/portal_designer.css") {
		for _, want := range []string{
			`--gjs-primary-color: #ffffff`,
			`--gjs-secondary-color: #161616`,
			`--gjs-left-width: 11rem`,
			`.designer .gjs-frame { background: #ffffff; }`,
			`.designer .gjs-editor { min-width: 48rem; }`,
		} {
			if !strings.Contains(got.body, want) {
				t.Errorf("%s: light editor theme missing %q", path, want)
			}
		}
	}
}
