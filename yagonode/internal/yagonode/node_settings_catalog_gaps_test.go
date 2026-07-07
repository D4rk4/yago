package yagonode

import (
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
