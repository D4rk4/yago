package publicportal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPortalShowsQueryTimeNextToResultCount(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	calls := 0
	oldClock := portalClock
	t.Cleanup(func() { portalClock = oldClock })
	portalClock = func() time.Time {
		calls++

		return base.Add(time.Duration(calls-1) * 420 * time.Millisecond)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "(0.42 s)") {
		t.Fatalf("query time missing from results meta: %s", rec.Body.String())
	}
}

func TestPortalRendersSkeletonMachinery(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`id="serp"`, "skel-line", "skel-shimmer",
		"prefers-reduced-motion", `aria-busy`, "Searching…",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("skeleton machinery missing %q", want)
		}
	}
}
