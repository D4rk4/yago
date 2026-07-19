package yagonode

import "testing"

type crawlerRuntimeSettingDefinitions struct {
	workers          settingDefinition
	processRate      settingDefinition
	maximumRedirects settingDefinition
	pageBudget       settingDefinition
	priority         settingDefinition
}

func TestCrawlerRuntimeSettingsAreLiveAndBounded(t *testing.T) {
	definitions := indexSettingDefinitions()
	settings := crawlerRuntimeSettingDefinitions{
		workers:          definitions[settingKeyCrawlerFetchWorkers],
		processRate:      definitions[settingKeyCrawlerProcessPagesPerSecond],
		maximumRedirects: definitions[settingKeyCrawlerMaximumRedirects],
		pageBudget:       definitions[settingKeyCrawlerMaxPagesPerRun],
		priority:         definitions[settingKeyPrioritizeAutomaticDiscovery],
	}
	requireLiveCrawlerRuntimeSettings(t, settings)
	requireCrawlerRuntimeSettingBounds(t, settings)
	requireAppliedCrawlerRuntimeSettings(t, settings)
	requireLiveCrawlerRuntimeValues(t, settings)
}

func requireLiveCrawlerRuntimeSettings(
	t *testing.T,
	settings crawlerRuntimeSettingDefinitions,
) {
	t.Helper()
	for _, definition := range []settingDefinition{
		settings.workers,
		settings.processRate,
		settings.maximumRedirects,
		settings.pageBudget,
		settings.priority,
	} {
		if definition.restartRequired() {
			t.Fatal("crawler runtime settings must apply live")
		}
	}
}

func requireCrawlerRuntimeSettingBounds(
	t *testing.T,
	settings crawlerRuntimeSettingDefinitions,
) {
	t.Helper()
	requireRejectedSettingValues(
		t, settings.maximumRedirects, "maximum redirects", "-1", "1001", "many",
	)
	requireNormalizedSettingValue(
		t, settings.maximumRedirects, " 0 ", "0", "maximum redirects",
	)
	requireRejectedSettingValues(
		t, settings.processRate, "process rate", "-1", "1000001", "many",
	)
	requireNormalizedSettingValue(t, settings.processRate, " 0 ", "0", "process rate")
	requireRejectedSettingValues(
		t, settings.workers, "worker concurrency", "0", "257", "many",
	)
	requireNormalizedSettingValue(t, settings.workers, " 20 ", "20", "workers")
	requireRejectedSettingValues(t, settings.pageBudget, "page budget", "-1", "many")
	requireNormalizedSettingValue(t, settings.pageBudget, " 0 ", "0", "page budget")
}

func requireRejectedSettingValues(
	t *testing.T,
	definition settingDefinition,
	name string,
	values ...string,
) {
	t.Helper()
	for _, value := range values {
		if _, err := definition.normalize(value); err == nil {
			t.Fatalf("%s %q was accepted", name, value)
		}
	}
}

func requireNormalizedSettingValue(
	t *testing.T,
	definition settingDefinition,
	raw string,
	want string,
	name string,
) {
	t.Helper()
	normalized, err := definition.normalize(raw)
	if err != nil || normalized != want {
		t.Fatalf("normalize %s = %q/%v, want %s/nil", name, normalized, err, want)
	}
}

func requireAppliedCrawlerRuntimeSettings(
	t *testing.T,
	settings crawlerRuntimeSettingDefinitions,
) {
	t.Helper()
	config := nodeConfig{Crawl: crawlConfig{
		FetchWorkers:                 4,
		ProcessPagesPerSecond:        10,
		MaximumRedirects:             10,
		MaxPagesPerRun:               50000,
		PrioritizeAutomaticDiscovery: true,
	}}
	config = settings.workers.apply(config, "20")
	config = settings.processRate.apply(config, "23")
	config = settings.maximumRedirects.apply(config, "7")
	config = settings.pageBudget.apply(config, "4321")
	config = settings.priority.apply(config, "false")
	if config.Crawl.FetchWorkers != 20 || config.Crawl.ProcessPagesPerSecond != 23 ||
		config.Crawl.MaximumRedirects != 7 ||
		config.Crawl.MaxPagesPerRun != 4321 ||
		config.Crawl.PrioritizeAutomaticDiscovery {
		t.Fatalf("applied crawler settings = %+v", config.Crawl)
	}
}

func requireLiveCrawlerRuntimeValues(
	t *testing.T,
	settings crawlerRuntimeSettingDefinitions,
) {
	t.Helper()
	toggles := newRuntimeToggles(nodeConfig{})
	liveWorkers := 0
	liveProcessRate := 0
	liveMaximumRedirects := 0
	livePageBudget := 0
	livePriority := true
	toggles.SetCrawlerFetchWorkersSink(func(value int) { liveWorkers = value })
	toggles.SetCrawlerProcessPagesPerSecondSink(func(value int) { liveProcessRate = value })
	toggles.SetCrawlerMaximumRedirectsSink(func(value int) { liveMaximumRedirects = value })
	toggles.SetCrawlerMaxPagesPerRunSink(func(value int) { livePageBudget = value })
	toggles.SetAutomaticDiscoveryPrioritySink(func(value bool) { livePriority = value })
	settings.workers.applyLive(toggles, "20")
	settings.processRate.applyLive(toggles, "23")
	settings.maximumRedirects.applyLive(toggles, "7")
	settings.pageBudget.applyLive(toggles, "4321")
	settings.priority.applyLive(toggles, settingBoolFalse)
	if liveWorkers != 20 || liveProcessRate != 23 || liveMaximumRedirects != 7 ||
		livePageBudget != 4321 || livePriority {
		t.Fatalf(
			"live crawler settings = workers %d process rate %d redirects %d pages %d priority %v",
			liveWorkers,
			liveProcessRate,
			liveMaximumRedirects,
			livePageBudget,
			livePriority,
		)
	}
}

func TestMaximumActiveCrawlTaskSettingIsLiveAndBounded(t *testing.T) {
	definition := indexSettingDefinitions()[settingKeyCrawlerMaximumActiveRuns]
	if definition.restartRequired() {
		t.Fatal("maximum active crawl tasks must apply live")
	}
	for _, value := range []string{"0", "257", "many"} {
		if _, err := definition.normalize(value); err == nil {
			t.Fatalf("active-run concurrency %q was accepted", value)
		}
	}
	if normalized, err := definition.normalize(" 32 "); err != nil || normalized != "32" {
		t.Fatalf("normalize active runs = %q/%v, want 32/nil", normalized, err)
	}
	baseline := nodeConfig{Crawl: crawlConfig{MaxActiveRuns: 32}}
	if value := definition.defaultValue(baseline); value != "32" {
		t.Fatalf("default active-run concurrency = %q, want 32", value)
	}
	config := definition.apply(nodeConfig{}, "37")
	if config.Crawl.MaxActiveRuns != 37 {
		t.Fatalf("applied active-run concurrency = %d, want 37", config.Crawl.MaxActiveRuns)
	}
	overridden := applyRuntimeSettingOverrides(baseline, map[string]string{
		settingKeyCrawlerMaximumActiveRuns: "37",
	})
	if overridden.Crawl.MaxActiveRuns != 37 {
		t.Fatalf("stored active-run concurrency = %d, want 37", overridden.Crawl.MaxActiveRuns)
	}
	toggles := newRuntimeToggles(nodeConfig{})
	liveMaximum := 0
	toggles.SetCrawlerMaximumActiveRunsSink(func(value int) { liveMaximum = value })
	definition.applyLive(toggles, "37")
	if liveMaximum != 37 {
		t.Fatalf("live active-run concurrency = %d, want 37", liveMaximum)
	}
}
