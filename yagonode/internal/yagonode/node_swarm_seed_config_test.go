package yagonode

import "testing"

func TestLoadNodeConfigReadsSwarmSeedSettings(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:           "0123456789AB",
		envPeerName:           "node",
		envSwarmSeedCrawl:     "true",
		envSwarmSeedLimitDocs: "500",
		envSwarmSeedDepth:     "3",
		envSwarmSeedMaxPages:  "75",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.SwarmSeed.Enabled || config.SwarmSeed.LimitDocs != 500 {
		t.Fatalf("SwarmSeed = %#v", config.SwarmSeed)
	}
	if config.SwarmSeed.SeedDepth != 3 || config.SwarmSeed.SeedMaxPages != 75 {
		t.Fatalf("autocrawler profile = %#v", config.SwarmSeed)
	}
}

func TestLoadNodeConfigSwarmSeedDefaultsOffWithDocLimit(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.SwarmSeed.Enabled || config.SwarmSeed.LimitDocs != defaultSwarmSeedLimitDocs {
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
		{envSwarmSeedLimitDocs: "many"},
		{envSwarmSeedLimitDocs: "0"},
		{envSwarmSeedLimitDocs: "-5"},
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
