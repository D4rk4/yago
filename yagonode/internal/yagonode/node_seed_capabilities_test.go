package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func assertFlagBits(t *testing.T, flags yagomodel.Flags, want map[int]bool) {
	t.Helper()
	for bit, expected := range want {
		if got := flags.Get(bit); got != expected {
			t.Errorf("flag bit %d = %v, want %v (flags=%q)", bit, got, expected, flags.String())
		}
	}
}

func TestSeedCapabilityFlagsDefaults(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.AdvertiseDirectConnect || !config.AdvertiseRemoteIndex {
		t.Errorf("defaults: direct=%v remoteIndex=%v, want both true",
			config.AdvertiseDirectConnect, config.AdvertiseRemoteIndex)
	}
	if config.AdvertiseRootNode || config.AdvertiseSSLAvailable {
		t.Errorf("defaults: root=%v ssl=%v, want both false",
			config.AdvertiseRootNode, config.AdvertiseSSLAvailable)
	}
	// Historical advertisement preserved; remote crawl never advertised (disabled).
	assertFlagBits(t, config.Flags, map[int]bool{
		yagomodel.FlagDirectConnect:     true,
		yagomodel.FlagAcceptRemoteIndex: true,
		yagomodel.FlagRootNode:          false,
		yagomodel.FlagSSLAvailable:      false,
		yagomodel.FlagAcceptRemoteCrawl: false,
	})
}

func TestSeedCapabilityFlagsReadEnvOverrides(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:             "0123456789AB",
		envPeerName:             "node",
		envAdvertiseDirect:      "false",
		envAdvertiseRemoteIndex: "false",
		envAdvertiseRootNode:    "true",
		envAdvertiseSSL:         "true",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	assertFlagBits(t, config.Flags, map[int]bool{
		yagomodel.FlagDirectConnect:     false,
		yagomodel.FlagAcceptRemoteIndex: false,
		yagomodel.FlagRootNode:          true,
		yagomodel.FlagSSLAvailable:      true,
		yagomodel.FlagAcceptRemoteCrawl: false,
	})
}

func TestSeedCapabilityFlagsRejectInvalidBool(t *testing.T) {
	_, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:          "0123456789AB",
		envPeerName:          "node",
		envAdvertiseRootNode: "notabool",
	}))
	if err == nil {
		t.Fatal("expected an error for an invalid boolean flag value")
	}
}

func TestConfigSeedFlagsRebuildsFromToggles(t *testing.T) {
	config := nodeConfig{
		AdvertiseDirectConnect: true,
		AdvertiseRemoteIndex:   false,
		AdvertiseRootNode:      true,
		AdvertiseSSLAvailable:  false,
	}
	assertFlagBits(t, configSeedFlags(config), map[int]bool{
		yagomodel.FlagDirectConnect:     true,
		yagomodel.FlagAcceptRemoteIndex: false,
		yagomodel.FlagRootNode:          true,
		yagomodel.FlagSSLAvailable:      false,
		yagomodel.FlagAcceptRemoteCrawl: false,
	})
}

func TestSeedCapabilitySettingsLiveInGeneralAndApply(t *testing.T) {
	defs := indexSettingDefinitions()
	cases := []struct {
		key  string
		bit  int
		read func(nodeConfig) bool
	}{
		{
			"peer.advertise.direct_connect",
			yagomodel.FlagDirectConnect,
			func(c nodeConfig) bool { return c.AdvertiseDirectConnect },
		},
		{
			"peer.advertise.remote_index",
			yagomodel.FlagAcceptRemoteIndex,
			func(c nodeConfig) bool { return c.AdvertiseRemoteIndex },
		},
		{
			"peer.advertise.root_node",
			yagomodel.FlagRootNode,
			func(c nodeConfig) bool { return c.AdvertiseRootNode },
		},
		{
			"peer.advertise.ssl",
			yagomodel.FlagSSLAvailable,
			func(c nodeConfig) bool { return c.AdvertiseSSLAvailable },
		},
	}
	for _, tc := range cases {
		def, ok := defs[tc.key]
		if !ok {
			t.Fatalf("missing setting %q", tc.key)
		}
		if got := settingCategory(tc.key); got != "General" {
			t.Errorf("%s category = %q, want General", tc.key, got)
		}
		updated := def.apply(nodeConfig{}, settingBoolTrue)
		if !tc.read(updated) {
			t.Errorf("%s: apply(true) did not set the config toggle", tc.key)
		}
		if !updated.Flags.Get(tc.bit) {
			t.Errorf("%s: apply(true) did not set flag bit %d", tc.key, tc.bit)
		}
		if updated.Flags.Get(yagomodel.FlagAcceptRemoteCrawl) {
			t.Errorf("%s: apply must never advertise remote crawl", tc.key)
		}
	}
}
