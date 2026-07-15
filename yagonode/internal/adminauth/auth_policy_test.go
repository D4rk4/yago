package adminauth

import (
	"net/http"
	"strings"
	"testing"
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
		!strings.Contains(page.Body.String(), `<link rel="stylesheet" href="/admin/auth.css">`) {
		t.Fatalf("login stylesheet markup = %s", page.Body.String())
	}

	stylesheet := doRequest(surface, http.MethodGet, PathAuthStylesheet, "")
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
}
