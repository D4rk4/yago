package yagonode

import (
	"strings"
	"testing"
)

func TestLANDiscoveryEnvironmentBootstrapsTheAdminSetting(t *testing.T) {
	defaults, err := loadNodeConfig(envFrom(map[string]string{}))
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if defaults.LANDiscovery {
		t.Fatal("LAN discovery defaulted on")
	}

	enabled, err := loadNodeConfig(envFrom(map[string]string{envLANDiscovery: "true"}))
	if err != nil {
		t.Fatalf("load enabled config: %v", err)
	}
	if !enabled.LANDiscovery {
		t.Fatal("LAN discovery environment value was ignored")
	}
	definition := indexSettingDefinitions()["network.lan_discovery"]
	if got := definition.defaultValue(enabled); got != settingBoolTrue {
		t.Fatalf("Admin bootstrap default = %q, want true", got)
	}
}

func TestLANDiscoveryEnvironmentRejectsInvalidBoolean(t *testing.T) {
	_, err := loadNodeConfig(envFrom(map[string]string{envLANDiscovery: "sometimes"}))
	if err == nil || !strings.Contains(err.Error(), envLANDiscovery) {
		t.Fatalf("LAN discovery error = %v", err)
	}
}
