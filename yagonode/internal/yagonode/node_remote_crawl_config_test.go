package yagonode

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteCrawlEnvironmentDefaultsAreDisabledAndBounded(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if config.RemoteCrawl.Enabled ||
		len(config.RemoteCrawl.TrustedPeers) != 0 ||
		len(config.RemoteCrawl.AllowedDestinations) != 0 ||
		config.Flags.Get(yagomodel.FlagAcceptRemoteCrawl) {
		t.Fatalf("remote crawl defaults = %+v flags=%q", config.RemoteCrawl, config.Flags)
	}
	if config.RemoteCrawl.RequestsPerMinute != remotecrawl.DefaultRequestsPerMinute ||
		config.RemoteCrawl.OutstandingPerPeer != remotecrawl.DefaultOutstandingPerPeer ||
		config.RemoteCrawl.LeaseTTL != remotecrawl.DefaultLeaseTTL ||
		config.RemoteCrawl.QueueCapacity != remotecrawl.DefaultQueueCapacity {
		t.Fatalf("remote crawl bounds = %+v", config.RemoteCrawl)
	}
}

func TestRemoteCrawlEnvironmentRequiresControlledTrustAndDestinations(t *testing.T) {
	base := map[string]string{envRemoteCrawlEnabled: "true"}
	cases := []map[string]string{
		base,
		mergeEnvironment(base, map[string]string{
			envNetworkAuthentication:         string(yagoproto.NetworkAuthenticationSaltedMagic),
			envNetworkAuthenticationMaterial: "shared",
		}),
		mergeEnvironment(base, map[string]string{
			envNetworkAuthentication:         string(yagoproto.NetworkAuthenticationSaltedMagic),
			envNetworkAuthenticationMaterial: "shared",
			envRemoteCrawlTrustedPeers:       "AAAAAAAAAAAA",
		}),
	}
	for position, environment := range cases {
		if _, err := loadNodeConfig(envFrom(environment)); err == nil {
			t.Fatalf("case %d enabled remote crawl without complete policy", position)
		}
	}
}

func TestRemoteCrawlEnvironmentEnablesProtocolCapability(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envRemoteCrawlEnabled:             "true",
		envRemoteCrawlTrustedPeers:        "AAAAAAAAAAAA,BBBBBBBBBBBB,AAAAAAAAAAAA",
		envRemoteCrawlAllowedDestinations: "Example.COM,10.20.0.0/16",
		envRemoteCrawlRequestsPerMinute:   "120",
		envRemoteCrawlOutstandingPerPeer:  "7",
		envRemoteCrawlLeaseTTL:            "5m",
		envRemoteCrawlQueueCapacity:       "900",
		envNetworkAuthentication:          string(yagoproto.NetworkAuthenticationSaltedMagic),
		envNetworkAuthenticationMaterial:  "shared",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !config.RemoteCrawl.Enabled || !config.Flags.Get(yagomodel.FlagAcceptRemoteCrawl) ||
		len(config.RemoteCrawl.TrustedPeers) != 2 ||
		strings.Join(config.RemoteCrawl.AllowedDestinations, ",") != "example.com,10.20.0.0/16" ||
		config.RemoteCrawl.RequestsPerMinute != 120 ||
		config.RemoteCrawl.OutstandingPerPeer != 7 ||
		config.RemoteCrawl.LeaseTTL != 5*time.Minute ||
		config.RemoteCrawl.QueueCapacity != 900 {
		t.Fatalf("remote crawl config = %+v flags=%q", config.RemoteCrawl, config.Flags)
	}
}

func TestRemoteCrawlEnvironmentBoundsEveryControl(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{envRemoteCrawlRequestsPerMinute, "0"},
		{envRemoteCrawlOutstandingPerPeer, "101"},
		{envRemoteCrawlLeaseTTL, "0s"},
		{envRemoteCrawlLeaseTTL, "25h"},
		{envRemoteCrawlQueueCapacity, "100001"},
		{envRemoteCrawlTrustedPeers, "short"},
		{envRemoteCrawlTrustedPeers, strings.Repeat("AAAAAAAAAAAA,", remotecrawl.MaximumTrustedPeers) + "AAAAAAAAAAAA"},
		{envRemoteCrawlAllowedDestinations, "*.example.com"},
		{envRemoteCrawlAllowedDestinations, strings.Repeat("example.com,", remotecrawl.MaximumAllowedDestinations) + "example.com"},
	} {
		if _, err := loadNodeConfig(envFrom(map[string]string{test.key: test.value})); err == nil {
			t.Fatalf("%s=%q accepted", test.key, test.value)
		}
	}
}

func TestRemoteCrawlEnvironmentRejectsMalformedEnable(t *testing.T) {
	t.Parallel()

	if _, err := loadRemoteCrawlConfig(envFrom(map[string]string{
		envRemoteCrawlEnabled: "not-a-boolean",
	})); err == nil {
		t.Fatal("malformed enable accepted")
	}
}

func TestRemoteCrawlSettingNormalizersHandleEmptyAndBoundaryValues(t *testing.T) {
	t.Parallel()

	if got, err := normalizeRemoteCrawlTrustedPeers(" "); err != nil || got != "" {
		t.Fatalf("empty trusted peers = %q, %v", got, err)
	}
	if got, err := normalizeRemoteCrawlDestinations(" "); err != nil || got != "" {
		t.Fatalf("empty destinations = %q, %v", got, err)
	}
	if got, err := normalizeRemoteCrawlLeaseTTL("2m"); err != nil || got != "2m0s" {
		t.Fatalf("lease TTL = %q, %v", got, err)
	}
	if got, err := normalizeRemoteCrawlInteger("42", 100); err != nil || got != "42" {
		t.Fatalf("integer = %q, %v", got, err)
	}
	invalid := []func() error{
		func() error { _, err := normalizeRemoteCrawlTrustedPeers("short"); return err },
		func() error { _, err := normalizeRemoteCrawlDestinations("*.example.com"); return err },
		func() error { _, err := normalizeRemoteCrawlLeaseTTL("invalid"); return err },
		func() error { _, err := normalizeRemoteCrawlInteger("invalid", 100); return err },
	}
	for position, parse := range invalid {
		if err := parse(); err == nil {
			t.Fatalf("invalid normalizer case %d accepted", position)
		}
	}
}

func TestRemoteCrawlSettingsPersistOnlyACompletePolicy(t *testing.T) {
	environment := nodeConfig{
		NetworkAuthenticationMode: yagoproto.NetworkAuthenticationUncontrolled,
		RemoteCrawl: remoteCrawlConfig{
			RequestsPerMinute:  remotecrawl.DefaultRequestsPerMinute,
			OutstandingPerPeer: remotecrawl.DefaultOutstandingPerPeer,
			LeaseTTL:           remotecrawl.DefaultLeaseTTL,
			QueueCapacity:      remotecrawl.DefaultQueueCapacity,
		},
	}
	source, store, _ := newTestSettingsSource(t, environment)
	ctx := context.Background()
	result, err := source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyRemoteCrawlEnabled, Value: settingBoolTrue,
	})
	if err != nil || result.OK || !strings.Contains(result.Message, "requires salted-magic") {
		t.Fatalf("unsafe enable = %+v, %v", result, err)
	}
	changes := []adminui.SettingsChange{
		{Key: settingKeyNetworkAuthenticationSecret, Value: "shared"},
		{
			Key:   settingKeyNetworkAuthenticationMode,
			Value: string(yagoproto.NetworkAuthenticationSaltedMagic),
		},
		{Key: settingKeyRemoteCrawlTrustedPeers, Value: "AAAAAAAAAAAA"},
		{Key: settingKeyRemoteCrawlAllowedDestinations, Value: "example.com"},
		{Key: settingKeyRemoteCrawlEnabled, Value: settingBoolTrue},
	}
	for _, change := range changes {
		result, err = source.Update(ctx, change)
		if err != nil || !result.OK || !result.RestartRequired {
			t.Fatalf("update %+v = %+v, %v", change, result, err)
		}
	}
	stored, set, err := store.Get(ctx, settingKeyRemoteCrawlEnabled)
	if err != nil || !set || stored != settingBoolTrue {
		t.Fatalf("stored enable = %q, %v, %v", stored, set, err)
	}
	result, err = source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyRemoteCrawlAllowedDestinations, Reset: true,
	})
	if err != nil || result.OK {
		t.Fatalf("unsafe destination reset = %+v, %v", result, err)
	}
	result, err = source.Update(ctx, adminui.SettingsChange{
		Key:   settingKeyNetworkAuthenticationMode,
		Value: string(yagoproto.NetworkAuthenticationUncontrolled),
	})
	if err != nil || result.OK {
		t.Fatalf("unsafe authentication downgrade = %+v, %v", result, err)
	}
}

func TestRemoteCrawlSettingCatalogMatchesEnvironmentControls(t *testing.T) {
	definitions := indexSettingDefinitions()
	matrix := []struct {
		environment string
		setting     string
	}{
		{envRemoteCrawlEnabled, settingKeyRemoteCrawlEnabled},
		{envRemoteCrawlTrustedPeers, settingKeyRemoteCrawlTrustedPeers},
		{envRemoteCrawlAllowedDestinations, settingKeyRemoteCrawlAllowedDestinations},
		{envRemoteCrawlRequestsPerMinute, settingKeyRemoteCrawlRequestsPerMinute},
		{envRemoteCrawlOutstandingPerPeer, settingKeyRemoteCrawlOutstandingPerPeer},
		{envRemoteCrawlLeaseTTL, settingKeyRemoteCrawlLeaseTTL},
		{envRemoteCrawlQueueCapacity, settingKeyRemoteCrawlQueueCapacity},
	}
	for _, control := range matrix {
		definition, found := definitions[control.setting]
		if !found || settingCategory(control.setting) != "Swarm" || !definition.restartRequired() {
			t.Errorf(
				"control %s/%s = found %v category %q restart %v",
				control.environment,
				control.setting,
				found,
				settingCategory(control.setting),
				definition.restartRequired(),
			)
		}
	}
}

func mergeEnvironment(base, additions map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(additions))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range additions {
		merged[key] = value
	}

	return merged
}
