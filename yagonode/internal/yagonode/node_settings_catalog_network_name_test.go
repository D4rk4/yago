package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestNetworkNameSettingPersistsAndRestoresBootstrap(t *testing.T) {
	environment := nodeConfig{NetworkName: "freeworld"}
	source, store, _ := newTestSettingsSource(t, environment)
	result, err := source.Update(
		context.Background(),
		adminui.SettingsChange{Key: settingKeyNetworkName, Value: " private "},
	)
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("update network name = %+v, err = %v", result, err)
	}
	stored, set, err := store.Get(context.Background(), settingKeyNetworkName)
	if err != nil || !set || stored != "private" {
		t.Fatalf("stored network name = %q/%v, err = %v", stored, set, err)
	}
	effective := applyRuntimeSettingOverrides(environment, map[string]string{
		settingKeyNetworkName: stored,
	})
	if effective.NetworkName != "private" {
		t.Fatalf("effective network name = %q", effective.NetworkName)
	}
	result, err = source.Update(
		context.Background(),
		adminui.SettingsChange{Key: settingKeyNetworkName, Reset: true},
	)
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("reset network name = %+v, err = %v", result, err)
	}
	if _, set, err := store.Get(context.Background(), settingKeyNetworkName); err != nil || set {
		t.Fatalf("network override survived reset: set = %v, err = %v", set, err)
	}
}

func TestNetworkNameValidationMatchesBootstrapAndAdmin(t *testing.T) {
	definition := indexSettingDefinitions()[settingKeyNetworkName]
	for _, value := range []string{
		"",
		"private\nnetwork",
		"private\u200bnetwork",
		"private\u2028network",
		"private\u2029network",
		string([]byte{'p', 'r', 'i', 'v', 'a', 't', 'e', 0xff}),
		strings.Repeat("n", maximumNetworkNameBytes+1),
	} {
		if _, err := definition.normalize(value); err == nil {
			t.Errorf("Admin accepted network name %q", value)
		}
		if value != "" {
			_, err := loadNodeConfig(envFrom(map[string]string{envNetworkName: value}))
			if err == nil {
				t.Errorf("bootstrap accepted network name %q", value)
			}
		}
	}
	config, err := loadNodeConfig(envFrom(map[string]string{envNetworkName: " private "}))
	if err != nil || config.NetworkName != "private" {
		t.Fatalf("bootstrap network name = %q, err = %v", config.NetworkName, err)
	}
}
