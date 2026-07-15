package yagonode

import "testing"

func TestAdminPublicSearchStatusTracksRuntimeToggles(t *testing.T) {
	t.Parallel()

	toggles := newRuntimeToggles(nodeConfig{
		PublicSearchUIEnabled: true,
		PublicBaseURL:         "https://one.example",
	})
	source := newAdminPublicSearchStatusSource(toggles, nodeConfig{})
	initial := source.PublicSearchStatus(t.Context())
	if !initial.Enabled || initial.BaseURL != "https://one.example" {
		t.Fatalf("initial status = %+v", initial)
	}

	toggles.SetPortalEnabled(false)
	toggles.SetPublicBaseURL("https://two.example")
	updated := source.PublicSearchStatus(t.Context())
	if updated.Enabled || updated.BaseURL != "https://two.example" {
		t.Fatalf("updated status = %+v", updated)
	}

	fallback := newAdminPublicSearchStatusSource(nil, nodeConfig{
		PublicSearchUIEnabled: true,
		PublicBaseURL:         "https://fallback.example",
	}).PublicSearchStatus(t.Context())
	if !fallback.Enabled || fallback.BaseURL != "https://fallback.example" {
		t.Fatalf("fallback status = %+v", fallback)
	}
}
