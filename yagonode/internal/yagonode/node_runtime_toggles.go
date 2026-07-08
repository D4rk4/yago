package yagonode

import (
	"sync/atomic"
	"time"

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
	greeting      atomic.Value
	// compaction holds the storage-compaction cadence in nanoseconds (0 = off);
	// the compaction loop reads the current value each cycle.
	compaction atomic.Int64
}

func newRuntimeToggles(config nodeConfig) *runtimeToggles {
	toggles := &runtimeToggles{}
	toggles.portalEnabled.Store(config.PublicSearchUIEnabled)
	toggles.httpsRedirect.Store(config.HTTPSRedirect)
	toggles.publicBaseURL.Store(config.PublicBaseURL)
	toggles.robotsPolicy.Store(string(publicrobots.ParsePolicy(config.RobotsPolicy)))
	toggles.greeting.Store(config.PortalGreeting)
	toggles.compaction.Store(int64(config.StorageCompaction))

	return toggles
}

// CompactionInterval is the live storage-compaction cadence (0 = off).
func (t *runtimeToggles) CompactionInterval() time.Duration {
	if t == nil {
		return 0
	}

	return time.Duration(t.compaction.Load())
}

// SetCompactionInterval changes the storage-compaction cadence without a
// restart; the compaction loop applies it on its next cycle.
func (t *runtimeToggles) SetCompactionInterval(interval time.Duration) {
	if t != nil {
		t.compaction.Store(int64(interval))
	}
}

// PortalGreeting is the live operator-chosen portal name ("" = default brand).
func (t *runtimeToggles) PortalGreeting() string {
	if t == nil {
		return ""
	}
	value, _ := t.greeting.Load().(string)

	return value
}

// SetPortalGreeting renames the portal without a restart.
func (t *runtimeToggles) SetPortalGreeting(value string) {
	t.greeting.Store(value)
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
