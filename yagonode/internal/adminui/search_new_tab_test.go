package adminui

import (
	"net/http"
	"strings"
	"testing"
)

func TestConsoleSearchLinksDefaultToSameTab(t *testing.T) {
	t.Parallel()

	console := New(Options{Search: fakeSearch{results: sampleResults()}})
	got := do(t, console, "/admin/search?q=go&scope=global")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if strings.Contains(got.body, `target="_blank"`) {
		t.Fatal("same-tab default should not render target=_blank")
	}
	if !strings.Contains(got.body, `rel="noreferrer nofollow"`) {
		t.Fatal("same-tab links should keep referrer and follow hygiene")
	}
}

func TestConsoleSearchLinksOpenNewTabWithIndicatorWhenEnabled(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Search:            fakeSearch{results: sampleResults()},
		SearchLinksNewTab: true,
	})
	got := do(t, console, "/admin/search?q=go&scope=global")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		`target="_blank"`,
		`rel="noopener noreferrer nofollow"`,
		"(opens in new tab)",
		"↗",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("new-tab search results missing %q", want)
		}
	}
}
