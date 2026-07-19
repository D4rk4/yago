package yagonode

import (
	"testing"
	"time"
)

func TestRemoteCrawlSettingDefinitionsApplyNormalizedValues(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		settingKeyRemoteCrawlTrustedPeers:        "0123456789AB",
		settingKeyRemoteCrawlAllowedDestinations: "example.org",
		settingKeyRemoteCrawlRequestsPerMinute:   "42",
		settingKeyRemoteCrawlOutstandingPerPeer:  "3",
		settingKeyRemoteCrawlLeaseTTL:            "2m",
		settingKeyRemoteCrawlQueueCapacity:       "99",
		settingKeyRemoteCrawlEnabled:             settingBoolTrue,
	}
	var config nodeConfig
	for _, definition := range remoteCrawlSettingDefinitions() {
		value, exists := values[definition.key]
		if !exists {
			t.Fatalf("missing test value for %q", definition.key)
		}
		normalized, err := definition.normalize(value)
		if err != nil {
			t.Fatalf("normalize %q: %v", definition.key, err)
		}
		config = definition.apply(config, normalized)
	}
	if len(config.RemoteCrawl.TrustedPeers) != 1 ||
		config.RemoteCrawl.AllowedDestinations[0] != "example.org" ||
		config.RemoteCrawl.RequestsPerMinute != 42 ||
		config.RemoteCrawl.OutstandingPerPeer != 3 ||
		config.RemoteCrawl.LeaseTTL != 2*time.Minute ||
		config.RemoteCrawl.QueueCapacity != 99 ||
		!config.RemoteCrawl.Enabled {
		t.Fatalf("applied remote-crawl config = %+v", config.RemoteCrawl)
	}
}
