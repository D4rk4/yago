package yagonode

import (
	"strings"
	"testing"
	"time"
)

type catalogRoundTripCase struct {
	value string
	check func(config nodeConfig) bool
}

func catalogRoundTripCases() map[string]catalogRoundTripCase {
	return map[string]catalogRoundTripCase{
		"peer.name": {"fresh-name", func(c nodeConfig) bool { return c.Name == "fresh-name" }},
		"network.advertise.host": {"pub.example", func(c nodeConfig) bool {
			return c.AdvertiseHost == "pub.example"
		}},
		"network.seedlists": {"https://a.example/s,https://b.example/s", func(c nodeConfig) bool {
			return len(c.SeedlistURLs) == 2 && c.SeedlistURLs[1] == "https://b.example/s"
		}},
		"search.links.newtab": {"true", func(c nodeConfig) bool { return c.SearchLinksNewTab }},
		"search.index.remote": {"true", func(c nodeConfig) bool { return c.IndexRemoteResults }},
		"metrics.enabled":     {"false", func(c nodeConfig) bool { return !c.MetricsEnabled }},
		"search.query.log": {"aggregate", func(c nodeConfig) bool {
			return c.QueryLogMode == queryLogAggregate
		}},
		"web.fallback.privacy": {"enabled", func(c nodeConfig) bool {
			return c.WebFallback.Privacy == webFallbackPrivacyEnabled
		}},
		"web.fallback.backend": {"brave", func(c nodeConfig) bool {
			return c.WebFallback.Backend == "brave"
		}},
		"web.fallback.seed_crawl": {"true", func(c nodeConfig) bool {
			return c.WebFallback.SeedCrawl
		}},
		"web.fallback.seed_depth": {"2", func(c nodeConfig) bool {
			return c.WebFallback.SeedDepth == 2
		}},
		"web.fallback.seed_max_pages": {"35", func(c nodeConfig) bool {
			return c.WebFallback.SeedMaxPages == 35
		}},
		"network.lan_discovery": {"false", func(c nodeConfig) bool {
			return !c.LANDiscovery
		}},
		"search.remote.peer_timeout": {"5s", func(c nodeConfig) bool {
			return c.RemotePeerTimeout == 5*time.Second
		}},
		"search.remote.timeout": {"6s", func(c nodeConfig) bool {
			return c.RemoteTimeout == 6*time.Second
		}},
		"swarm.seed.enabled": {"true", func(c nodeConfig) bool { return c.SwarmSeed.Enabled }},
		"swarm.seed.depth": {
			"4",
			func(c nodeConfig) bool { return c.SwarmSeed.SeedDepth == 4 },
		},
		"swarm.seed.max_pages": {
			"60",
			func(c nodeConfig) bool { return c.SwarmSeed.SeedMaxPages == 60 },
		},
		"extract.fetch.enabled": {"true", func(c nodeConfig) bool {
			return c.ExtractFetch.Enabled
		}},
	}
}

func TestExtendedSettingCatalogRoundTrips(t *testing.T) {
	base := nodeConfig{
		Name:               "old-name",
		AdvertiseHost:      "old.example",
		SeedlistURLs:       []string{"https://seeds.example/a"},
		SearchLinksNewTab:  false,
		IndexRemoteResults: false,
		MetricsEnabled:     true,
		QueryLogMode:       queryLogOff,
	}
	base.WebFallback.Privacy = webFallbackPrivacyDisabled

	cases := catalogRoundTripCases()
	byKey := indexSettingDefinitions()
	for key, tc := range cases {
		def, ok := byKey[key]
		if !ok {
			t.Fatalf("setting %q missing from the catalog", key)
		}
		normalized, err := def.normalize(tc.value)
		if err != nil {
			t.Fatalf("%s normalize: %v", key, err)
		}
		if !tc.check(def.apply(base, normalized)) {
			t.Fatalf("%s apply did not take effect", key)
		}
		if def.defaultValue(base) == "" && key != "swarm.seed.enabled" &&
			key != "search.links.newtab" && key != "search.index.remote" &&
			key != "extract.fetch.enabled" {
			if key == "peer.name" || key == "network.advertise.host" ||
				key == "network.seedlists" {
				t.Fatalf("%s default must reflect the config", key)
			}
		}
	}

	// Overrides layer through the shared startup path too.
	layered := applyRuntimeSettingOverrides(base, map[string]string{
		"peer.name":        "layered",
		"swarm.seed.depth": "4",
	})
	if layered.Name != "layered" || layered.SwarmSeed.SeedDepth != 4 {
		t.Fatalf("layered = %+v", layered)
	}
}

func TestExtendedSettingValidation(t *testing.T) {
	byKey := indexSettingDefinitions()
	if _, err := byKey["peer.name"].normalize("two\nlines"); err == nil {
		t.Fatal("multi-line value must fail")
	}
	if _, err := byKey["search.query.log"].normalize("noisy"); err == nil {
		t.Fatal("unknown log mode must fail")
	}
	if _, err := byKey["web.fallback.privacy"].normalize("sometimes"); err == nil {
		t.Fatal("unknown privacy mode must fail")
	}
	if _, err := byKey["web.fallback.backend"].normalize("google"); err == nil {
		t.Fatal("unsupported backend must fail")
	}
	if normalized, err := byKey["web.fallback.backend"].normalize(" DDG "); err != nil ||
		normalized != "ddg" {
		t.Fatalf("backend normalize = %q %v", normalized, err)
	}
	if normalized, err := byKey["web.fallback.privacy"].normalize(" Explicit "); err != nil ||
		normalized != "explicit" {
		t.Fatalf("privacy normalize = %q %v", normalized, err)
	}

	// No secret ever appears in the catalog.
	for key := range byKey {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "key") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
			t.Fatalf("secret-looking setting exposed: %s", key)
		}
	}

	// Extended settings without live hooks require a restart.
	if !byKey["peer.name"].restartRequired() {
		t.Fatal("peer.name must require a restart")
	}
}

func TestAutocrawlerSettingValidation(t *testing.T) {
	byKey := indexSettingDefinitions()
	for _, key := range []string{"swarm.seed.depth", "web.fallback.seed_depth"} {
		if _, err := byKey[key].normalize("99"); err == nil {
			t.Fatalf("%s: out-of-range depth must fail", key)
		}
		if _, err := byKey[key].normalize("-1"); err == nil {
			t.Fatalf("%s: negative depth must fail", key)
		}
		if normalized, err := byKey[key].normalize(" 0 "); err != nil || normalized != "0" {
			t.Fatalf("%s: depth 0 must normalize: %q %v", key, normalized, err)
		}
	}
	for _, key := range []string{"swarm.seed.max_pages", "web.fallback.seed_max_pages"} {
		if _, err := byKey[key].normalize("0"); err == nil {
			t.Fatalf("%s: non-positive page cap must fail", key)
		}
	}
}

func TestRemoteTimeoutSettingValidation(t *testing.T) {
	byKey := indexSettingDefinitions()
	for _, bad := range []string{"soon", "50ms", "10m"} {
		if _, err := byKey["search.remote.timeout"].normalize(bad); err == nil {
			t.Fatalf("%q must fail duration validation", bad)
		}
	}
	if normalized, err := byKey["search.remote.peer_timeout"].normalize(" 3s "); err != nil ||
		normalized != "3s" {
		t.Fatalf("duration normalize = %q %v", normalized, err)
	}
}
