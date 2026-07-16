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
	// autosplit gates automatic shard-pool growth; the growth loop reads it each
	// cycle (ADR-0037).
	autosplit atomic.Bool
	// storageQuota carries a new disk-budget ceiling to the vault. It is wired at
	// boot to vault.SetQuota and holds a func(int64); a storage.quota admin change
	// flows through it so the new ceiling applies without a restart (ADR-0037 D).
	storageQuota               atomic.Value
	crawlerFetchWorkers        atomic.Value
	automaticDiscoveryPriority atomic.Value
}

func newRuntimeToggles(config nodeConfig) *runtimeToggles {
	toggles := &runtimeToggles{}
	toggles.portalEnabled.Store(config.PublicSearchUIEnabled)
	toggles.httpsRedirect.Store(config.HTTPSRedirect)
	toggles.publicBaseURL.Store(config.PublicBaseURL)
	toggles.robotsPolicy.Store(string(publicrobots.ParsePolicy(config.RobotsPolicy)))
	toggles.greeting.Store(config.PortalGreeting)
	toggles.compaction.Store(int64(config.StorageCompaction))
	toggles.autosplit.Store(config.StorageAutosplit)

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

// AutosplitEnabled reports whether the storage shard pool grows automatically.
func (t *runtimeToggles) AutosplitEnabled() bool {
	return t != nil && t.autosplit.Load()
}

// SetAutosplitEnabled turns automatic shard growth on or off without a restart;
// the growth loop applies it on its next cycle.
func (t *runtimeToggles) SetAutosplitEnabled(enabled bool) {
	if t != nil {
		t.autosplit.Store(enabled)
	}
}

// SetQuotaSink wires the callback that carries a new storage quota to the vault.
// It is set once at boot, before any admin request can fire ApplyStorageQuota.
func (t *runtimeToggles) SetQuotaSink(sink func(int64)) {
	if t != nil && sink != nil {
		t.storageQuota.Store(sink)
	}
}

// ApplyStorageQuota pushes a new disk-budget ceiling to the vault when a sink is
// wired; the eviction sweep honors the new ceiling on its next cycle (ADR-0037 D).
func (t *runtimeToggles) ApplyStorageQuota(quotaBytes int64) {
	if t == nil {
		return
	}
	if sink, ok := t.storageQuota.Load().(func(int64)); ok {
		sink(quotaBytes)
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
