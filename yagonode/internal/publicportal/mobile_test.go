package publicportal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalMobileLayoutMachinery(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		// Results pages pin the search bar for thumb re-query on small screens.
		`class="topbar"`, "position: sticky",
		// Touch controls grow to the 44px minimum under the mobile breakpoint.
		"@media (max-width: 48rem)", "height: 2.75rem",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mobile machinery missing %q", want)
		}
	}
}

func TestPortalHomepageKeepsCenteredLayout(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="home"`) {
		t.Fatal("homepage lost its centered layout class")
	}
	if strings.Contains(body, `class="topbar"`) {
		t.Fatal("homepage must not render the sticky results top bar")
	}
}
