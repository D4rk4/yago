package yagonode

import "testing"

func TestCrawlerRuntimeSettingsAreLiveAndBounded(t *testing.T) {
	definitions := indexSettingDefinitions()
	workers := definitions[settingKeyCrawlerFetchWorkers]
	pageBudget := definitions[settingKeyCrawlerMaxPagesPerRun]
	priority := definitions[settingKeyPrioritizeAutomaticDiscovery]
	if workers.restartRequired() || pageBudget.restartRequired() || priority.restartRequired() {
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
	for _, value := range []string{"-1", "many"} {
		if _, err := pageBudget.normalize(value); err == nil {
			t.Fatalf("page budget %q was accepted", value)
		}
	}
	if normalized, err := pageBudget.normalize(" 0 "); err != nil || normalized != "0" {
		t.Fatalf("normalize page budget = %q/%v, want 0/nil", normalized, err)
	}
	config := nodeConfig{Crawl: crawlConfig{
		FetchWorkers:                 4,
		MaxPagesPerRun:               50000,
		PrioritizeAutomaticDiscovery: true,
	}}
	config = workers.apply(config, "20")
	config = pageBudget.apply(config, "4321")
	config = priority.apply(config, "false")
	if config.Crawl.FetchWorkers != 20 || config.Crawl.MaxPagesPerRun != 4321 ||
		config.Crawl.PrioritizeAutomaticDiscovery {
		t.Fatalf("applied crawler settings = %+v", config.Crawl)
	}
	toggles := newRuntimeToggles(nodeConfig{})
	liveWorkers := 0
	livePageBudget := 0
	livePriority := true
	toggles.SetCrawlerFetchWorkersSink(func(value int) { liveWorkers = value })
	toggles.SetCrawlerMaxPagesPerRunSink(func(value int) { livePageBudget = value })
	toggles.SetAutomaticDiscoveryPrioritySink(func(value bool) { livePriority = value })
	workers.applyLive(toggles, "20")
	pageBudget.applyLive(toggles, "4321")
	priority.applyLive(toggles, settingBoolFalse)
	if liveWorkers != 20 || livePageBudget != 4321 || livePriority {
		t.Fatalf("live crawler settings = workers %d pages %d priority %v",
			liveWorkers, livePageBudget, livePriority)
	}
}
