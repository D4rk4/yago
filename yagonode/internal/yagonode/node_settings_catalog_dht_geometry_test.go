package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestDHTGeometryAndTuningSettingsMatchEnvironmentBounds(t *testing.T) {
	environment := nodeConfig{DHT: dhtDistributionConfig{
		Interval:   10,
		Redundancy: defaultDHTRedundancy, PartitionExponent: defaultDHTPartitionExponent,
	}}
	source, _, _ := newTestSettingsSource(t, environment)

	accepted := []adminui.SettingsChange{
		{Key: settingKeyDHTPartitionExponent, Value: "0"},
		{Key: "dht.redundancy", Value: "16"},
		{Key: "dht.min_peer_age_days", Value: "-1"},
		{Key: "dht.interval", Value: "10s"},
	}
	for _, change := range accepted {
		result, err := source.Update(context.Background(), change)
		if err != nil || !result.OK || !result.RestartRequired {
			t.Fatalf("accepted update %+v = %+v, error = %v", change, result, err)
		}
	}

	rejected := []adminui.SettingsChange{
		{Key: settingKeyDHTPartitionExponent, Value: "9"},
		{Key: "dht.redundancy", Value: "17"},
		{Key: "dht.min_peer_age_days", Value: "-2"},
		{Key: "dht.interval", Value: "0s"},
	}
	for _, change := range rejected {
		result, err := source.Update(context.Background(), change)
		if err != nil || result.OK {
			t.Fatalf("rejected update %+v = %+v, error = %v", change, result, err)
		}
	}
}

func TestDHTPartitionExponentOverrideAppliesAtRestart(t *testing.T) {
	environment := nodeConfig{DHT: dhtDistributionConfig{PartitionExponent: 4}}
	effective := applyRuntimeSettingOverrides(environment, map[string]string{
		settingKeyDHTPartitionExponent: "2",
	})
	if effective.DHT.PartitionExponent != 2 {
		t.Fatalf("partition exponent = %d, want 2", effective.DHT.PartitionExponent)
	}
}
