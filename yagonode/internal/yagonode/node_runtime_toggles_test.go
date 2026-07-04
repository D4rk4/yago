package yagonode

import "testing"

func TestNewRuntimeTogglesReadsConfig(t *testing.T) {
	t.Parallel()

	toggles := newRuntimeToggles(nodeConfig{PublicSearchUIEnabled: true, HTTPSRedirect: false})
	if !toggles.PortalEnabled() {
		t.Fatal("portal toggle should start from the effective config value")
	}
	if toggles.HTTPSRedirectEnabled() {
		t.Fatal("redirect toggle should start disabled by default")
	}
}

func TestRuntimeTogglesSet(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	toggles.SetPortalEnabled(true)
	toggles.SetHTTPSRedirect(true)
	if !toggles.PortalEnabled() || !toggles.HTTPSRedirectEnabled() {
		t.Fatal("setters did not update the toggle state")
	}
}
