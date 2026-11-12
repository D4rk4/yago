package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAdminSearchRedirectKeepsUntrustedQueryInsideFixedLocation(t *testing.T) {
	t.Parallel()

	query := "//outside.example/\r\nLocation: https://outside.example/?x=1#escape"
	recorder := httptest.NewRecorder()
	redirectAdminSearchPage(recorder, query, false, 7)

	location := recorder.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if recorder.Code != http.StatusSeeOther || parsed.IsAbs() || parsed.Host != "" ||
		parsed.Path != searchPath || parsed.Fragment != "" {
		t.Fatalf("redirect = %d %q", recorder.Code, location)
	}
	if parsed.Query().Get("q") != query || parsed.Query().Get("scope") != "local" ||
		parsed.Query().Get("p") != "7" {
		t.Fatalf("redirect query = %q", parsed.RawQuery)
	}
	if strings.ContainsAny(location, "\r\n") ||
		strings.Contains(location, "outside.example/?x=1#") {
		t.Fatalf("untrusted query escaped encoding: %q", location)
	}
}

func TestCrawlRunRedirectUsesFixedCrawlerLocation(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	redirectCrawlRunPage(recorder)
	if location := recorder.Header().Get("Location"); location != "/admin/crawl#crawl-monitor" {
		t.Fatalf("Location = %q", location)
	}
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
}
