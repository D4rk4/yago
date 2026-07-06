package yagonode

import "sync/atomic"

// runtimeToggles holds the live-appliable operator switches that the admin
// console can flip without a restart: whether the public search portal is served
// at the site root, and whether plain-HTTP requests are redirected to HTTPS. The
// serving handlers read the current value on every request.
type runtimeToggles struct {
	portalEnabled atomic.Bool
	httpsRedirect atomic.Bool
	publicBaseURL atomic.Value
}

func newRuntimeToggles(config nodeConfig) *runtimeToggles {
	toggles := &runtimeToggles{}
	toggles.portalEnabled.Store(config.PublicSearchUIEnabled)
	toggles.httpsRedirect.Store(config.HTTPSRedirect)
	toggles.publicBaseURL.Store(config.PublicBaseURL)

	return toggles
}

func (t *runtimeToggles) PortalEnabled() bool {
	return t != nil && t.portalEnabled.Load()
}

func (t *runtimeToggles) SetPortalEnabled(enabled bool) {
	t.portalEnabled.Store(enabled)
}

func (t *runtimeToggles) HTTPSRedirectEnabled() bool {
	return t != nil && t.httpsRedirect.Load()
}

func (t *runtimeToggles) SetHTTPSRedirect(enabled bool) {
	t.httpsRedirect.Store(enabled)
}

// PublicBaseURL returns the operator-configured public origin, or empty when
// URLs derive from each request.
func (t *runtimeToggles) PublicBaseURL() string {
	if t == nil {
		return ""
	}
	value, _ := t.publicBaseURL.Load().(string)

	return value
}

// SetPublicBaseURL applies a new public origin immediately.
func (t *runtimeToggles) SetPublicBaseURL(value string) {
	if t != nil {
		t.publicBaseURL.Store(value)
	}
}
