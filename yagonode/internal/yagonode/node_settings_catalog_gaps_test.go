package yagonode

import (
	"strings"
	"testing"
	"time"
)

// catalogGapRoundTripCases pins CFG-02: every setting the parity review added
// applies its value onto the runtime config.
func catalogGapRoundTripCases() map[string]catalogRoundTripCase {
	return map[string]catalogRoundTripCase{
		"storage.quota": {"50GB", func(c nodeConfig) bool {
			return c.StorageQuotaByte == 50<<30
		}},
		"search.api.scoped_access": {"true", func(c nodeConfig) bool {
			return c.SearchRequireAPIKey
		}},
		"network.peer.https_preferred": {"false", func(c nodeConfig) bool {
			return !c.PeerHTTPSPreferred
		}},
		"network.announce.interval": {"15m", func(c nodeConfig) bool {
			return c.AnnounceInterval == 15*time.Minute
		}},
		"network.announce.greets_per_cycle": {"7", func(c nodeConfig) bool {
			return c.GreetsPerCycle == 7
		}},
		"dht.enabled": {"false", func(c nodeConfig) bool {
			return !c.DHT.Gates.NetworkDHTEnabled
		}},
		"dht.distribution": {"false", func(c nodeConfig) bool {
			return !c.DHT.Gates.DistributionEnabled
		}},
		"dht.allow_while_crawling": {"true", func(c nodeConfig) bool {
			return c.DHT.Gates.AllowWhileCrawling
		}},
		"dht.allow_while_indexing": {"true", func(c nodeConfig) bool {
			return c.DHT.Gates.AllowWhileIndexing
		}},
		"dht.interval": {
			"90s",
			func(c nodeConfig) bool { return c.DHT.Interval == 90*time.Second },
		},
		"dht.redundancy": {"5", func(c nodeConfig) bool { return c.DHT.Redundancy == 5 }},
		"dht.min_peer_age_days": {"0", func(c nodeConfig) bool {
			return c.DHT.MinimumPeerAgeDays == 0
		}},
		"dht.min_connected_peers": {"3", func(c nodeConfig) bool {
			return c.DHT.Gates.MinimumConnectedPeer == 3
		}},
		"dht.min_rwi_words": {"200", func(c nodeConfig) bool {
			return c.DHT.Gates.MinimumRWIWord == 200
		}},
		"web.fallback.max_results": {"15", func(c nodeConfig) bool {
			return c.WebFallback.MaxResults == 15
		}},
		"web.fallback.timeout": {"8s", func(c nodeConfig) bool {
			return c.WebFallback.Timeout == 8*time.Second
		}},
		"web.fallback.cache_ttl": {"10m", func(c nodeConfig) bool {
			return c.WebFallback.CacheTTL == 10*time.Minute
		}},
		"web.fallback.safesearch": {"strict", func(c nodeConfig) bool {
			return c.WebFallback.SafeSearch == "strict"
		}},
		"extract.fetch.timeout": {"20s", func(c nodeConfig) bool {
			return c.ExtractFetch.Timeout == 20*time.Second
		}},
		"extract.fetch.max_bytes": {"1048576", func(c nodeConfig) bool {
			return c.ExtractFetch.MaxBytes == 1<<20
		}},
		"security.egress.allow_private": {"true", func(c nodeConfig) bool {
			return c.EgressAllowLAN
		}},
		"security.egress.allow_cidrs": {"10.0.0.0/8", func(c nodeConfig) bool {
			return len(c.EgressAllowedCIDRs) == 1 &&
				c.EgressAllowedCIDRs[0].String() == "10.0.0.0/8"
		}},
		"security.trusted_proxies": {"192.168.0.0/16", func(c nodeConfig) bool {
			return len(c.TrustedProxies) == 1 && c.TrustedProxies[0].String() == "192.168.0.0/16"
		}},
		"security.cors.admin": {"https://ui.example", func(c nodeConfig) bool {
			return len(c.CrossOrigin.AdminOrigins) == 1 &&
				c.CrossOrigin.AdminOrigins[0] == "https://ui.example"
		}},
		"security.cors.search": {"https://app.example", func(c nodeConfig) bool {
			return len(c.CrossOrigin.SearchOrigins) == 1 &&
				c.CrossOrigin.SearchOrigins[0] == "https://app.example"
		}},
	}
}

func TestParityGapSettingsRoundTrip(t *testing.T) {
	base := nodeConfig{StorageQuotaByte: 1 << 30, GreetsPerCycle: 3}
	base.DHT.Gates.NetworkDHTEnabled = true
	base.DHT.Gates.DistributionEnabled = true
	definitions := map[string]settingDefinition{}
	for _, definition := range extendedSettingDefinitions() {
		definitions[definition.key] = definition
	}

	for key, testCase := range catalogGapRoundTripCases() {
		definition, ok := definitions[key]
		if !ok {
			t.Fatalf("%s: missing from the catalog", key)
		}
		normalized, err := definition.normalize(testCase.value)
		if err != nil {
			t.Fatalf("%s: normalize(%q): %v", key, testCase.value, err)
		}
		if !testCase.check(definition.apply(base, normalized)) {
			t.Fatalf("%s: value %q did not reach the config", key, normalized)
		}
		if definition.defaultValue == nil {
			t.Fatalf("%s: no default renderer", key)
		}
	}
}

func TestParityGapNormalizersReject(t *testing.T) {
	rejects := map[string][2]string{
		"storage.quota":             {"", "10MB"},
		"network.announce.interval": {"5s", "200h"},
		"dht.min_peer_age_days":     {"-1", "x"},
		"web.fallback.safesearch":   {"maximum", ""},
		"security.trusted_proxies":  {"not-a-cidr", "10.0.0.0/8,zzz"},
	}
	definitions := map[string]settingDefinition{}
	for _, definition := range extendedSettingDefinitions() {
		definitions[definition.key] = definition
	}
	for key, values := range rejects {
		for _, value := range values {
			if _, err := definitions[key].normalize(value); err == nil {
				t.Fatalf("%s: %q must be rejected", key, value)
			}
		}
	}
}

func TestFormatByteSizeRendersUnits(t *testing.T) {
	if got := formatByteSize(50 << 30); got != "50GB" {
		t.Fatalf("50GB = %q", got)
	}
	if got := formatByteSize(1536); got != "1536" {
		t.Fatalf("odd size = %q", got)
	}
}

// TestRobotsPolicySettingRoundTrips pins UI-15 (ConfigRobotsTxt parity): the
// robots policy is editable, validated, applies to the config, and flips the
// live toggle without a restart.
func TestRobotsPolicySettingRoundTrips(t *testing.T) {
	var definition settingDefinition
	for _, candidate := range allRuntimeSettingDefinitions() {
		if candidate.key == "web.robots.policy" {
			definition = candidate
		}
	}
	if definition.key == "" {
		t.Fatal("web.robots.policy missing from the catalog")
	}
	normalized, err := definition.normalize(" Closed ")
	if err != nil || normalized != "closed" {
		t.Fatalf("normalize = %q %v", normalized, err)
	}
	if _, err := definition.normalize("everything"); err == nil {
		t.Fatal("unknown policy must be rejected")
	}
	applied := definition.apply(nodeConfig{}, "closed")
	if applied.RobotsPolicy != "closed" {
		t.Fatalf("apply = %+v", applied)
	}
	toggles := newRuntimeToggles(nodeConfig{})
	if got := toggles.RobotsPolicy(); string(got) != "no-serp" {
		t.Fatalf("default policy = %q", got)
	}
	definition.applyLive(toggles, "closed")
	if got := toggles.RobotsPolicy(); string(got) != "closed" {
		t.Fatalf("live apply = %q", got)
	}
	var nilToggles *runtimeToggles
	if got := nilToggles.RobotsPolicy(); string(got) != "no-serp" {
		t.Fatalf("nil toggles policy = %q", got)
	}
}

// TestSearchRateSettingsRoundTrip pins UI-20 (SearchAccessRate_p parity):
// each tier setting edits its own field, untouched fields keep the shipped
// defaults, and a zero-valued config resolves to the defaults.
func TestSearchRateSettingsRoundTrip(t *testing.T) {
	definitions := map[string]settingDefinition{}
	for _, definition := range extendedSettingDefinitions() {
		definitions[definition.key] = definition
	}
	burst, ok := definitions["search.rate.burst"]
	if !ok {
		t.Fatal("search.rate.burst missing from the catalog")
	}
	if got := burst.defaultValue(nodeConfig{}); got != "10" {
		t.Fatalf("default burst = %q, want shipped default", got)
	}
	applied := burst.apply(nodeConfig{}, "25")
	if applied.SearchRate.Per3Seconds != 25 || applied.SearchRate.PerMinute != 60 ||
		applied.SearchRate.Per10Minutes != 300 {
		t.Fatalf("apply = %+v", applied.SearchRate)
	}
	applied = definitions["search.rate.minute"].apply(applied, "120")
	if applied.SearchRate.Per3Seconds != 25 || applied.SearchRate.PerMinute != 120 {
		t.Fatalf("second apply lost the first: %+v", applied.SearchRate)
	}
	if _, err := definitions["search.rate.ten_minutes"].normalize("0"); err == nil {
		t.Fatal("zero limit must be rejected")
	}
}

// TestPortalGreetingSetting pins UI-21: the name normalizes, rejects markup,
// applies to the config, and flips the live toggle.
func TestPortalGreetingSetting(t *testing.T) {
	var definition settingDefinition
	for _, candidate := range allRuntimeSettingDefinitions() {
		if candidate.key == "portal.greeting" {
			definition = candidate
		}
	}
	if definition.key == "" {
		t.Fatal("portal.greeting missing from the catalog")
	}
	if normalized, err := definition.normalize("  My Library  "); err != nil ||
		normalized != "My Library" {
		t.Fatalf("normalize = %q %v", normalized, err)
	}
	if _, err := definition.normalize("<script>"); err == nil {
		t.Fatal("markup must be rejected")
	}
	if _, err := definition.normalize(strings.Repeat("x", 61)); err == nil {
		t.Fatal("over-long name must be rejected")
	}
	toggles := newRuntimeToggles(nodeConfig{})
	definition.applyLive(toggles, "My Library")
	if toggles.PortalGreeting() != "My Library" {
		t.Fatalf("live apply = %q", toggles.PortalGreeting())
	}
	if definition.apply(nodeConfig{}, "My Library").PortalGreeting != "My Library" {
		t.Fatal("config apply lost the value")
	}
}
