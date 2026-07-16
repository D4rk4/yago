package yagonode

import "testing"

func TestCrawlerRuntimeSettingsAreLiveAndBounded(t *testing.T) {
	definitions := indexSettingDefinitions()
	workers := definitions[settingKeyCrawlerFetchWorkers]
	priority := definitions[settingKeyPrioritizeAutomaticDiscovery]
	if workers.restartRequired() || priority.restartRequired() {
		t.Fatal("crawler runtime settings must apply live")
	}
	for _, value := range []string{"0", "257", "many"} {
		if _, err := workers.normalize(value); err == nil {
			t.Fatalf("worker concurrency %q was accepted", value)
		}
	}
	if normalized, err := workers.normalize(" 20 "); err != nil || normalized != "20" {
		t.Fatalf("normalize workers = %q/%v, want 20/nil", normalized, err)
	}
	config := nodeConfig{Crawl: crawlConfig{
		FetchWorkers:                 4,
		PrioritizeAutomaticDiscovery: true,
	}}
	config = workers.apply(config, "20")
	config = priority.apply(config, "false")
	if config.Crawl.FetchWorkers != 20 || config.Crawl.PrioritizeAutomaticDiscovery {
		t.Fatalf("applied crawler settings = %+v", config.Crawl)
	}
	toggles := newRuntimeToggles(nodeConfig{})
	liveWorkers := 0
	livePriority := true
	toggles.SetCrawlerFetchWorkersSink(func(value int) { liveWorkers = value })
	toggles.SetAutomaticDiscoveryPrioritySink(func(value bool) { livePriority = value })
	workers.applyLive(toggles, "20")
	priority.applyLive(toggles, settingBoolFalse)
	if liveWorkers != 20 || livePriority {
		t.Fatalf("live crawler settings = workers %d priority %v", liveWorkers, livePriority)
	}
}
