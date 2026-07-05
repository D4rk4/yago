package publicportal

import (
	"strings"
	"testing"
)

func portalResults() *fakeSource {
	return &fakeSource{results: SearchResults{
		Query:        "go",
		TotalResults: 1,
		Results: []SearchResult{{
			Title: "Go Result",
			URL:   "https://example.org/go",
		}},
	}}
}

func TestPortalLinksDefaultToSameTab(t *testing.T) {
	_, body := get(t, New(portalResults(), false), "/?q=go")
	if strings.Contains(body, `target="_blank"`) {
		t.Fatal("same-tab default should not render target=_blank")
	}
	if !strings.Contains(body, `rel="noreferrer nofollow"`) {
		t.Fatal("same-tab links should keep referrer and follow hygiene")
	}
}

func TestPortalLinksOpenNewTabWithIndicatorWhenEnabled(t *testing.T) {
	_, body := get(t, New(portalResults(), true), "/?q=go")
	for _, want := range []string{
		`target="_blank"`,
		`rel="noopener noreferrer nofollow"`,
		"(opens in new tab)",
		"↗",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("new-tab portal results missing %q", want)
		}
	}
}
