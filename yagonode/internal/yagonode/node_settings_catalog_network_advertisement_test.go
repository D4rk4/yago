package yagonode

import (
	"context"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestNetworkAdvertisementSettingsPreserveDerivedDefaults(t *testing.T) {
	selfTest, err := url.Parse("http://127.0.0.1:8090")
	if err != nil {
		t.Fatal(err)
	}
	environment := nodeConfig{
		PeerAddr:          ":8090",
		AdvertisePort:     8090,
		PublicSelfTestURL: selfTest,
	}
	source, _, _ := newTestSettingsSource(t, environment)

	for _, key := range []string{
		settingKeyNetworkAdvertisePort,
		settingKeyNetworkPublicSelfTest,
	} {
		item := settingViewItem(t, source.Settings(context.Background()), key)
		if item.Value != "" || item.Overridden || !item.RestartRequired {
			t.Fatalf("item %q = %+v, want derived restart-required default", key, item)
		}
	}
}

func TestNetworkAdvertisementSettingsPinAndApply(t *testing.T) {
	environment := nodeConfig{PeerAddr: ":8090", AdvertisePort: 8090}
	source, _, _ := newTestSettingsSource(t, environment)

	changes := []adminui.SettingsChange{
		{Key: settingKeyNetworkAdvertisePort, Value: "9443"},
		{Key: settingKeyNetworkPublicSelfTest, Value: "https://peer.example:9443/base"},
	}
	for _, change := range changes {
		result, err := source.Update(context.Background(), change)
		if err != nil || !result.OK || !result.RestartRequired {
			t.Fatalf("update %q = %+v, error = %v", change.Key, result, err)
		}
	}

	effective := applyRuntimeSettingOverrides(environment, map[string]string{
		settingKeyNetworkAdvertisePort:  "9443",
		settingKeyNetworkPublicSelfTest: "https://peer.example:9443/base",
	})
	effective = realignPeerDerivedConfig(effective)
	if effective.AdvertisePort != 9443 || !effective.AdvertisePortPinned {
		t.Fatalf("advertised port = %d/%v", effective.AdvertisePort, effective.AdvertisePortPinned)
	}
	if effective.PublicSelfTestURL == nil ||
		effective.PublicSelfTestURL.String() != "https://peer.example:9443/base" ||
		!effective.SelfTestURLPinned {
		t.Fatalf("self-test URL = %v/%v", effective.PublicSelfTestURL, effective.SelfTestURLPinned)
	}
}

func TestNetworkAdvertisementSettingsRejectInvalidValues(t *testing.T) {
	source, _, _ := newTestSettingsSource(t, nodeConfig{})
	for _, change := range []adminui.SettingsChange{
		{Key: settingKeyNetworkAdvertisePort, Value: "65536"},
		{Key: settingKeyNetworkAdvertisePort, Value: "zero"},
		{Key: settingKeyNetworkPublicSelfTest, Value: "file:///tmp/node"},
		{Key: settingKeyNetworkPublicSelfTest, Value: "https:///missing-host"},
	} {
		result, err := source.Update(context.Background(), change)
		if err != nil || result.OK {
			t.Fatalf("invalid update %+v = %+v, error = %v", change, result, err)
		}
	}
}

func TestAdvertisePortRejectsOutOfRangeEnvironmentValue(t *testing.T) {
	_, err := loadNodeConfig(envFrom(map[string]string{
		envAdvertisePort: "65536",
	}))
	if err == nil {
		t.Fatal("out-of-range advertised port was accepted")
	}
}

func TestNetworkAdvertisementDefinitionsExposePinnedDefaultsAndReset(t *testing.T) {
	t.Parallel()

	selfTest, err := url.Parse("https://peer.example:9443/base")
	if err != nil {
		t.Fatal(err)
	}
	config := nodeConfig{
		AdvertisePort:       9443,
		AdvertisePortPinned: true,
		PublicSelfTestURL:   selfTest,
		SelfTestURLPinned:   true,
	}
	definitions := make(map[string]settingDefinition)
	for _, definition := range networkAdvertisementDefinitions() {
		definitions[definition.key] = definition
	}
	if got := definitions[settingKeyNetworkAdvertisePort].defaultValue(config); got != "9443" {
		t.Fatalf("advertised-port default = %q", got)
	}
	if got := definitions[settingKeyNetworkPublicSelfTest].defaultValue(config); got !=
		selfTest.String() {
		t.Fatalf("self-test default = %q", got)
	}
	config = definitions[settingKeyNetworkAdvertisePort].apply(config, "")
	config = definitions[settingKeyNetworkPublicSelfTest].apply(config, "")
	if config.AdvertisePortPinned || config.AdvertisePort != 0 ||
		config.SelfTestURLPinned || config.PublicSelfTestURL != nil {
		t.Fatalf("reset config = %+v", config)
	}
}
