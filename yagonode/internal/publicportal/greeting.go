package publicportal

import "sync/atomic"

// Portal branding — YaCy ConfigPortal_p greeting parity (UI-21). The operator
// names the portal; results and behavior are untouched.

var greetingProvider atomic.Pointer[func() string]

// SetGreetingProvider installs the live portal-name provider; the default
// brand stays when none is set or the provider returns an empty string.
func SetGreetingProvider(provider func() string) {
	greetingProvider.Store(&provider)
}

// portalBrand resolves the display name for one render.
func portalBrand() string {
	provider := greetingProvider.Load()
	if provider == nil {
		return brand
	}
	if custom := (*provider)(); custom != "" {
		return custom
	}

	return brand
}
