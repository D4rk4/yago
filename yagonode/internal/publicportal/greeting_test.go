package publicportal

import "testing"

// TestPortalBrandFollowsProvider pins UI-21: without a provider the built-in
// brand renders, a provider's non-empty name wins live, and an empty provider
// value falls back to the brand.
func TestPortalBrandFollowsProvider(t *testing.T) {
	t.Cleanup(func() { SetGreetingProvider(func() string { return "" }) })

	name := ""
	SetGreetingProvider(func() string { return name })
	if got := portalBrand(); got != brand {
		t.Fatalf("empty provider = %q, want default brand", got)
	}
	name = "Моя библиотека"
	if got := portalBrand(); got != "Моя библиотека" {
		t.Fatalf("named portal = %q", got)
	}
}
