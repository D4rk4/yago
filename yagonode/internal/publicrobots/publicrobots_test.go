package publicrobots

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsePolicyFallsBackToNoSERP(t *testing.T) {
	cases := map[string]Policy{
		"open":    PolicyOpen,
		"CLOSED":  PolicyClosed,
		"no-serp": PolicyNoSERP,
		"":        PolicyNoSERP,
		"typo":    PolicyNoSERP,
	}
	for raw, want := range cases {
		if got := ParsePolicy(raw); got != want {
			t.Fatalf("ParsePolicy(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := ParsePolicy("  closed\t"); got != PolicyClosed {
		t.Fatalf("surrounding whitespace must be trimmed: %q", got)
	}
}

// TestBodyPerPolicy pins the three payloads: the default hides every
// query-addressed surface including portal ?q= searches, open allows all,
// closed shuts the site.
func TestBodyPerPolicy(t *testing.T) {
	noSERP := Body(PolicyNoSERP)
	for _, path := range []string{
		"Disallow: /yacysearch.html", "Disallow: /yacysearch.json",
		"Disallow: /yacysearch.rss", "Disallow: /suggest.json",
		"Disallow: /api/", "Disallow: /*?q=",
	} {
		if !strings.Contains(noSERP, path) {
			t.Fatalf("no-serp body misses %q:\n%s", path, noSERP)
		}
	}
	if strings.Contains(noSERP, "Disallow: /\n") {
		t.Fatal("no-serp must not close the site root")
	}
	if open := Body(PolicyOpen); !strings.Contains(open, "Disallow:\n") ||
		strings.Contains(open, "Disallow: /") {
		t.Fatalf("open body = %q", open)
	}
	if closed := Body(PolicyClosed); !strings.Contains(closed, "Disallow: /\n") {
		t.Fatalf("closed body = %q", closed)
	}
}

// TestMountServesLivePolicy pins the runtime behavior: the handler re-reads
// the policy per request, so a settings change applies without a restart.
func TestMountServesLivePolicy(t *testing.T) {
	policy := PolicyNoSERP
	mux := http.NewServeMux()
	Mount(mux, func() Policy { return policy })

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/robots.txt", nil,
	))
	if first.Code != http.StatusOK ||
		!strings.Contains(first.Body.String(), "/yacysearch.html") {
		t.Fatalf("first = %d %q", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content type = %q", got)
	}

	policy = PolicyClosed
	second := httptest.NewRecorder()
	mux.ServeHTTP(second, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/robots.txt", nil,
	))
	if !strings.Contains(second.Body.String(), "Disallow: /\n") {
		t.Fatalf("live change not applied: %q", second.Body.String())
	}
}
