package yagonode

import (
	"strings"
	"testing"
)

func TestNetworkIdentitySettingsUseBootstrapValidators(t *testing.T) {
	definitions := indexSettingDefinitions()
	cases := []struct {
		key      string
		accepted string
		expected string
		rejected string
	}{
		{"peer.name", " node-ä ", "node-ä", strings.Repeat("a", maximumPeerNameBytes+1)},
		{"network.advertise.host", "Example.COM", "example.com", "example.com:8090"},
		{
			"network.seedlists",
			"HTTPS://Example.COM:443/seeds",
			"https://example.com/seeds",
			"file:///tmp/seeds",
		},
	}
	for _, test := range cases {
		definition := definitions[test.key]
		got, err := definition.normalize(test.accepted)
		if err != nil || got != test.expected {
			t.Fatalf("%s normalize = %q, %v, want %q", test.key, got, err, test.expected)
		}
		if _, err := definition.normalize(test.rejected); err == nil {
			t.Fatalf("%s accepted %q", test.key, test.rejected)
		}
	}
}

func TestNetworkCadenceSettingsMatchBootstrapBounds(t *testing.T) {
	definitions := indexSettingDefinitions()
	for key, accepted := range map[string][]string{
		"network.announce.interval":         {"30s", "168h"},
		"network.announce.greets_per_cycle": {"1", "1024"},
		"dht.interval":                      {"1s"},
	} {
		for _, value := range accepted {
			if _, err := definitions[key].normalize(value); err != nil {
				t.Fatalf("%s rejected %q: %v", key, value, err)
			}
		}
	}
	for key, rejected := range map[string][]string{
		"network.announce.interval":         {"29s", "168h1s"},
		"network.announce.greets_per_cycle": {"0", "1025"},
		"dht.interval":                      {"999ms"},
	} {
		for _, value := range rejected {
			if _, err := definitions[key].normalize(value); err == nil {
				t.Fatalf("%s accepted %q", key, value)
			}
		}
	}
}
