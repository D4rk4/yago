package yagonode

import (
	"testing"
	"time"
)

func TestDurationRangeEnvironmentBoundaries(t *testing.T) {
	for value, expected := range map[string]time.Duration{
		"100ms": 100 * time.Millisecond,
		"2m":    2 * time.Minute,
	} {
		got, err := durationRangeEnv(
			func(string) string { return value },
			"BOUND",
			time.Second,
			minimumInteractiveSearchTimeout,
			maximumInteractiveSearchTimeout,
		)
		if err != nil || got != expected {
			t.Fatalf("duration %q = %v, %v", value, got, err)
		}
	}
	for _, value := range []string{"99ms", "2m1ms", "invalid"} {
		if _, err := durationRangeEnv(
			func(string) string { return value },
			"BOUND",
			time.Second,
			minimumInteractiveSearchTimeout,
			maximumInteractiveSearchTimeout,
		); err == nil {
			t.Fatalf("duration %q must fail", value)
		}
	}
}

func TestNodeDurationBootstrapBounds(t *testing.T) {
	base := map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
	}
	for key, values := range map[string][]string{
		envRemotePeerTimeout:       {"99ms", "2m1ms"},
		envRemoteTimeout:           {"99ms", "2m1ms"},
		envAnnounceInterval:        {"29s", "168h1s"},
		envDHTDistributionInterval: {"999ms"},
	} {
		for _, value := range values {
			environment := map[string]string{key: value}
			for name, configured := range base {
				environment[name] = configured
			}
			if _, err := loadNodeConfig(envFrom(environment)); err == nil {
				t.Fatalf("%s=%s must fail", key, value)
			}
		}
	}
}

func TestNodeDurationBootstrapAcceptsExactBounds(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:                "0123456789AB",
		envPeerName:                "node",
		envRemotePeerTimeout:       "100ms",
		envRemoteTimeout:           "2m",
		envAnnounceInterval:        "168h",
		envDHTDistributionInterval: "1s",
	}))
	if err != nil {
		t.Fatalf("load exact duration bounds: %v", err)
	}
	if config.RemotePeerTimeout != 100*time.Millisecond ||
		config.RemoteTimeout != 2*time.Minute ||
		config.AnnounceInterval != 168*time.Hour ||
		config.DHT.Interval != time.Second {
		t.Fatalf(
			"duration bounds = %v/%v/%v/%v",
			config.RemotePeerTimeout,
			config.RemoteTimeout,
			config.AnnounceInterval,
			config.DHT.Interval,
		)
	}
}
