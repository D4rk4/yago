package yagonode

import "testing"

func TestRuntimeCrawlerSettingsReachWiredSinks(t *testing.T) {
	toggles := newRuntimeToggles(nodeConfig{})
	workers := 0
	activeRuns := 0
	pageBudget := 0
	priority := false
	toggles.SetCrawlerFetchWorkersSink(func(value int) { workers = value })
	toggles.SetCrawlerMaximumActiveRunsSink(func(value int) { activeRuns = value })
	toggles.SetCrawlerMaxPagesPerRunSink(func(value int) { pageBudget = value })
	toggles.SetAutomaticDiscoveryPrioritySink(func(value bool) { priority = value })
	toggles.ApplyCrawlerFetchWorkers(18)
	toggles.ApplyCrawlerMaximumActiveRuns(37)
	toggles.ApplyCrawlerMaxPagesPerRun(1234)
	toggles.ApplyAutomaticDiscoveryPriority(true)
	if workers != 18 || activeRuns != 37 || pageBudget != 1234 || !priority {
		t.Fatalf(
			"crawler live settings = workers %d active runs %d pages %d priority %v",
			workers,
			activeRuns,
			pageBudget,
			priority,
		)
	}

	var nilToggles *runtimeToggles
	nilToggles.SetCrawlerFetchWorkersSink(func(int) {})
	nilToggles.SetCrawlerMaximumActiveRunsSink(func(int) {})
	nilToggles.SetCrawlerMaxPagesPerRunSink(func(int) {})
	nilToggles.SetAutomaticDiscoveryPrioritySink(func(bool) {})
	nilToggles.ApplyCrawlerFetchWorkers(1)
	nilToggles.ApplyCrawlerMaximumActiveRuns(1)
	nilToggles.ApplyCrawlerMaxPagesPerRun(1)
	nilToggles.ApplyAutomaticDiscoveryPriority(true)
}

func TestRuntimeCrawlerSettingsAttachToLiveRuntime(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	toggles := newRuntimeToggles(nodeConfig{})
	attachCrawlRuntimeSettings(runtime, toggles)

	toggles.ApplyCrawlerFetchWorkers(18)
	if got := runtime.controlRegistry().RuntimeSnapshot().FetchLimitPerCrawler; got != 18 {
		t.Fatalf("live crawler fetch workers = %d, want 18", got)
	}
	toggles.ApplyCrawlerMaximumActiveRuns(37)
	if got := runtime.controlRegistry().MaximumActiveRuns(); got != 37 {
		t.Fatalf("live crawler active-run limit = %d, want 37", got)
	}
	toggles.ApplyCrawlerMaxPagesPerRun(2345)
	if got := runtime.MaxPagesPerRun(); got != 2345 {
		t.Fatalf("live crawler max pages per run = %d, want 2345", got)
	}
	toggles.ApplyAutomaticDiscoveryPriority(false)
	toggles.ApplyAutomaticDiscoveryPriority(true)
}
