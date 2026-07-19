package yagonode

import "github.com/D4rk4/yago/yagonode/internal/crawlbroker"

type crawlRuntimeSettingsSource interface {
	controlRegistry() *crawlbroker.ControlRegistry
	automaticDiscoveryQueue() *crawlbroker.DurableOrderQueue
	SetMaxPagesPerRun(int)
}

func (t *runtimeToggles) SetCrawlerFetchWorkersSink(sink func(int)) {
	if t != nil && sink != nil {
		t.crawlerFetchWorkers.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerFetchWorkers(workers int) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerFetchWorkers.Load().(func(int)); ok {
		sink(workers)
	}
}

func (t *runtimeToggles) SetCrawlerProcessPagesPerSecondSink(sink func(int)) {
	if t != nil && sink != nil {
		t.crawlerProcessPagesPerSecond.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerProcessPagesPerSecond(pagesPerSecond int) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerProcessPagesPerSecond.Load().(func(int)); ok {
		sink(pagesPerSecond)
	}
}

func (t *runtimeToggles) SetCrawlerMaximumRedirectsSink(sink func(int)) {
	if t != nil && sink != nil {
		t.crawlerMaximumRedirects.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerMaximumRedirects(maximum int) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerMaximumRedirects.Load().(func(int)); ok {
		sink(maximum)
	}
}

func (t *runtimeToggles) SetCrawlerMaximumActiveRunsSink(sink func(int)) {
	if t != nil && sink != nil {
		t.crawlerMaximumActiveRuns.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerMaximumActiveRuns(maximum int) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerMaximumActiveRuns.Load().(func(int)); ok {
		sink(maximum)
	}
}

func (t *runtimeToggles) SetCrawlerMaxPagesPerRunSink(sink func(int)) {
	if t != nil && sink != nil {
		t.crawlerMaxPagesPerRun.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerMaxPagesPerRun(value int) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerMaxPagesPerRun.Load().(func(int)); ok {
		sink(value)
	}
}

func (t *runtimeToggles) SetAutomaticDiscoveryPrioritySink(sink func(bool)) {
	if t != nil && sink != nil {
		t.automaticDiscoveryPriority.Store(sink)
	}
}

func (t *runtimeToggles) ApplyAutomaticDiscoveryPriority(enabled bool) {
	if t == nil {
		return
	}
	if sink, ok := t.automaticDiscoveryPriority.Load().(func(bool)); ok {
		sink(enabled)
	}
}

func attachCrawlRuntimeSettings(runtime crawlProcess, toggles *runtimeToggles) {
	source, ok := runtime.(crawlRuntimeSettingsSource)
	if toggles == nil || !ok {
		return
	}
	control := source.controlRegistry()
	queue := source.automaticDiscoveryQueue()
	toggles.SetCrawlerFetchWorkersSink(func(workers int) {
		control.SetFetchWorkers(workers)
	})
	toggles.SetCrawlerProcessPagesPerSecondSink(func(pagesPerSecond int) {
		control.SetProcessPagesPerSecond(pagesPerSecond)
	})
	toggles.SetCrawlerMaximumRedirectsSink(func(maximum int) {
		control.SetMaximumRedirects(maximum)
	})
	toggles.SetCrawlerMaximumActiveRunsSink(func(maximum int) {
		control.SetMaximumActiveRuns(maximum)
	})
	toggles.SetCrawlerMaxPagesPerRunSink(source.SetMaxPagesPerRun)
	toggles.SetCrawlerRuntimePolicySink(control.SetRuntimePolicy)
	toggles.SetCrawlerStoragePressureSink(control.SetStoragePressurePolicy)
	toggles.SetAutomaticDiscoveryPrioritySink(func(enabled bool) {
		queue.SetAutomaticDiscoveryPriority(enabled)
		control.SetAutomaticDiscoveryPriority(enabled)
	})
}

func (r *crawlRuntime) automaticDiscoveryQueue() *crawlbroker.DurableOrderQueue {
	return r.broker.Orders
}
