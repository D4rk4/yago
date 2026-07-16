package adminauth

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAuthPagesUseStrictPolicyAndExternalStyles(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "operator", "correct-horse")
	surface := htmlSurface(t, service)
	page := doRequest(surface, http.MethodGet, PathLoginPage, "")
	if page.Code != http.StatusOK {
		t.Fatalf("login status = %d", page.Code)
	}
	if page.Header().Get("Cache-Control") != authPageCache ||
		page.Header().Get("Content-Security-Policy") != authContentPolicy ||
		page.Header().Get("X-Content-Type-Options") != "nosniff" ||
		page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("login policy headers = %#v", page.Header())
	}
	if strings.Contains(page.Body.String(), "<style") ||
		!strings.Contains(
			page.Body.String(),
			`<link rel="stylesheet" href="`+authStylesheetReference+`">`,
		) {
		t.Fatalf("login stylesheet markup = %s", page.Body.String())
	}

	stylesheet := doRequest(surface, http.MethodGet, authStylesheetReference, "")
	if stylesheet.Code != http.StatusOK ||
		stylesheet.Header().Get("Content-Type") != "text/css; charset=utf-8" ||
		stylesheet.Header().Get("Cache-Control") != authStylesheetCache ||
		stylesheet.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stylesheet response = %d %#v", stylesheet.Code, stylesheet.Header())
	}
	expectedStylesheet, err := authTemplateFS.ReadFile("assets/auth.css")
	if err != nil {
		t.Fatalf("read embedded stylesheet: %v", err)
	}
	if stylesheet.Body.String() != string(expectedStylesheet) {
		t.Fatal("stylesheet response differs from the embedded asset")
	}

	unversioned := doRequest(surface, http.MethodGet, PathAuthStylesheet, "")
	if unversioned.Code != http.StatusOK ||
		unversioned.Header().Get("Cache-Control") != authStylesheetRevalidateCache {
		t.Fatalf("unversioned stylesheet = %d %#v", unversioned.Code, unversioned.Header())
	}
	rejected := doRequest(surface, http.MethodGet, PathAuthStylesheet+"?v=stale", "")
	if rejected.Code != http.StatusNotFound ||
		rejected.Header().Get("Cache-Control") != authStylesheetRejectedCache {
		t.Fatalf("stale stylesheet = %d %#v", rejected.Code, rejected.Header())
	}
}

func TestAuthStylesheetRevisionRequiresEmbeddedAsset(t *testing.T) {
	t.Parallel()

	deferred := false
	func() {
		defer func() { deferred = recover() != nil }()
		_ = mustAuthStylesheetRevision(fstest.MapFS{})
	}()
	if !deferred {
		t.Fatal("missing auth stylesheet did not panic")
	}
	if _, err := fs.ReadFile(authTemplateFS, "assets/auth.css"); err != nil {
		t.Fatal(err)
	}
}
