package publicportal

import "sync/atomic"

// baseURLProvider, when set, supplies the operator-configured public origin
// (reverse-proxy deployments); an empty result falls back to the request.
var baseURLProvider atomic.Pointer[func() string]

// SetBaseURLProvider installs the public-origin provider for absolute URLs.
func SetBaseURLProvider(provider func() string) {
	if provider != nil {
		baseURLProvider.Store(&provider)
	}
}

// configuredBaseURL returns the provider's origin, or empty.
func configuredBaseURL() string {
	if provider := baseURLProvider.Load(); provider != nil {
		return (*provider)()
	}

	return ""
}
