package yagonode

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchremote"
)

func TestLoadNodeConfigReadsSwarmSeedSettings(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:          "0123456789AB",
		envPeerName:          "node",
		envSwarmSeedCrawl:    "true",
		envSwarmSeedDepth:    "3",
		envSwarmSeedMaxPages: "75",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.SwarmSeed.Enabled {
		t.Fatalf("SwarmSeed = %#v", config.SwarmSeed)
	}
	if config.SwarmSeed.SeedDepth != 3 || config.SwarmSeed.SeedMaxPages != 75 {
		t.Fatalf("autocrawler profile = %#v", config.SwarmSeed)
	}
}

func TestLoadNodeConfigSwarmSeedDefaultsOff(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.SwarmSeed.Enabled {
		t.Fatalf("SwarmSeed = %#v", config.SwarmSeed)
	}
	if config.SwarmSeed.SeedDepth != defaultSwarmSeedDepth ||
		config.SwarmSeed.SeedMaxPages != defaultSwarmSeedMaxPages {
		t.Fatalf("autocrawler defaults = %#v", config.SwarmSeed)
	}
}

func TestLoadNodeConfigRejectsInvalidSwarmSeedSettings(t *testing.T) {
	for _, item := range []map[string]string{
		{envSwarmSeedCrawl: "maybe"},
		{envSwarmSeedDepth: "-1"},
		{envSwarmSeedDepth: "99"},
		{envSwarmSeedDepth: "deep"},
		{envSwarmSeedMaxPages: "0"},
		{envSwarmSeedMaxPages: "lots"},
	} {
		env := map[string]string{envPeerHash: "0123456789AB", envPeerName: "node"}
		for key, value := range item {
			env[key] = value
		}
		if _, err := loadNodeConfig(envFrom(env)); err == nil {
			t.Fatalf("load config error = nil, want an error for %v", item)
		}
	}
}

func TestLoadNodeConfigReadsSearchLinksNewTab(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:          "0123456789AB",
		envPeerName:          "node",
		envSearchLinksNewTab: "true",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.SearchLinksNewTab {
		t.Error("SearchLinksNewTab = false, want true when enabled")
	}
	if _, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:          "0123456789AB",
		envPeerName:          "node",
		envSearchLinksNewTab: "maybe",
	})); err == nil {
		t.Fatal("load config error = nil, want an error for an unparseable boolean")
	}
}

func TestLoadNodeConfigReadsRemoteSearchTimeouts(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:          "0123456789AB",
		envPeerName:          "node",
		envRemotePeerTimeout: "5s",
		envRemoteTimeout:     "7s",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.RemotePeerTimeout != 5*time.Second || config.RemoteTimeout != 7*time.Second {
		t.Fatalf("remote timeouts = %v/%v", config.RemotePeerTimeout, config.RemoteTimeout)
	}

	defaults, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
	}))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if defaults.RemotePeerTimeout != searchremote.DefaultPerPeerTimeout ||
		defaults.RemoteTimeout != searchremote.DefaultOverallTimeout {
		t.Fatalf("default timeouts = %v/%v", defaults.RemotePeerTimeout, defaults.RemoteTimeout)
	}

	if _, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:      "0123456789AB",
		envPeerName:      "node",
		envRemoteTimeout: "soon",
	})); err == nil {
		t.Fatal("unparseable remote timeout must fail")
	}
}
