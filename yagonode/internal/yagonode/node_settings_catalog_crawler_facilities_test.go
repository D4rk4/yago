package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func TestCrawlerProcessFacilitySettingsPersistAndPropagate(t *testing.T) {
	environment := nodeConfig{Crawl: crawlConfig{
		RuntimePolicy: yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	}}
	source, store, _ := newTestSettingsSource(t, environment)
	policies := make([]yagocrawlcontract.CrawlerRuntimePolicy, 0, 3)
	source.toggles.SetCrawlerRuntimePolicySink(
		func(policy yagocrawlcontract.CrawlerRuntimePolicy) bool {
			policies = append(policies, policy)

			return true
		},
	)
	changes := []adminui.SettingsChange{
		{Key: settingKeyCrawlerBrowserPath, Value: "/usr/bin/firefox-esr"},
		{Key: settingKeyCrawlerMetricsAddress, Value: "127.0.0.1:9101"},
	}
	for _, change := range changes {
		result, err := source.Update(context.Background(), change)
		if err != nil || !result.OK || result.RestartRequired {
			t.Fatalf("update %s = %+v, err = %v", change.Key, result, err)
		}
		stored, set, readErr := store.Get(context.Background(), change.Key)
		if readErr != nil || !set || stored != change.Value {
			t.Fatalf("stored %s = %q/%v, err = %v", change.Key, stored, set, readErr)
		}
	}
	last := policies[len(policies)-1]
	if last.BrowserPath != changes[0].Value || last.MetricsAddress != changes[1].Value {
		t.Fatalf("propagated crawler facilities = %+v", last)
	}
}

func TestCrawlerProcessFacilitySettingsRejectInvalidValuesAndReset(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.BrowserPath = "/opt/firefox/firefox"
	policy.MetricsAddress = "127.0.0.1:9100"
	source, store, _ := newTestSettingsSource(t, nodeConfig{Crawl: crawlConfig{
		RuntimePolicy: policy,
	}})
	for key, value := range map[string]string{
		settingKeyCrawlerBrowserPath:    "firefox",
		settingKeyCrawlerMetricsAddress: "localhost:9101",
	} {
		result, err := source.Update(
			context.Background(),
			adminui.SettingsChange{Key: key, Value: value},
		)
		if err != nil || result.OK {
			t.Fatalf("invalid update %s = %+v, err = %v", key, result, err)
		}
	}
	if result, err := source.Update(
		context.Background(),
		adminui.SettingsChange{Key: settingKeyCrawlerBrowserPath, Value: ""},
	); err != nil || !result.OK {
		t.Fatalf("empty browser path update = %+v, err = %v", result, err)
	}
	if result, err := source.Update(
		context.Background(),
		adminui.SettingsChange{Key: settingKeyCrawlerBrowserPath, Reset: true},
	); err != nil || !result.OK {
		t.Fatalf("browser path reset = %+v, err = %v", result, err)
	}
	if _, set, err := store.Get(
		context.Background(),
		settingKeyCrawlerBrowserPath,
	); err != nil || set {
		t.Fatalf("browser path override survived reset: set = %v, err = %v", set, err)
	}
}
