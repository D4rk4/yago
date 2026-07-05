package yagonode

import "testing"

func TestLoadNodeConfigReadsSwarmSeedSettings(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:           "0123456789AB",
		envPeerName:           "node",
		envSwarmSeedCrawl:     "true",
		envSwarmSeedLimitDocs: "500",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.SwarmSeed.Enabled || config.SwarmSeed.LimitDocs != 500 {
		t.Fatalf("SwarmSeed = %#v", config.SwarmSeed)
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
}

func TestLoadNodeConfigRejectsInvalidSwarmSeedSettings(t *testing.T) {
	for _, item := range []map[string]string{
		{envSwarmSeedCrawl: "maybe"},
		{envSwarmSeedLimitDocs: "many"},
		{envSwarmSeedLimitDocs: "0"},
		{envSwarmSeedLimitDocs: "-5"},
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
