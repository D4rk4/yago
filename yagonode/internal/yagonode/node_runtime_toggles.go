package yagonode

import (
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/publicrobots"
)

// runtimeToggles holds the live-appliable operator switches that the admin
// console can flip without a restart: whether the public search portal is served
// at the site root, and whether plain-HTTP requests are redirected to HTTPS. The
// serving handlers read the current value on every request.
type runtimeToggles struct {
	portalEnabled atomic.Bool
	httpsRedirect atomic.Bool
	publicBaseURL atomic.Value
	robotsPolicy  atomic.Value
}

func newRuntimeToggles(config nodeConfig) *runtimeToggles {
	toggles := &runtimeToggles{}
	toggles.portalEnabled.Store(config.PublicSearchUIEnabled)
	toggles.httpsRedirect.Store(config.HTTPSRedirect)
	toggles.publicBaseURL.Store(config.PublicBaseURL)
	toggles.robotsPolicy.Store(string(publicrobots.ParsePolicy(config.RobotsPolicy)))

	return toggles
}

// RobotsPolicy is the live foreign-crawler policy for the public listener.
func (t *runtimeToggles) RobotsPolicy() publicrobots.Policy {
	if t == nil {
		return publicrobots.PolicyNoSERP
	}
	value, _ := t.robotsPolicy.Load().(string)

	return publicrobots.ParsePolicy(value)
}

// SetRobotsPolicy applies a robots-policy change without a restart.
func (t *runtimeToggles) SetRobotsPolicy(value string) {
	t.robotsPolicy.Store(string(publicrobots.ParsePolicy(value)))
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
