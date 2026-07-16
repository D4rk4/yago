package yagonode

import "github.com/D4rk4/yago/yagonode/internal/crawlbroker"

type crawlRuntimeSettingsSource interface {
	controlRegistry() *crawlbroker.ControlRegistry
	automaticDiscoveryQueue() *crawlbroker.DurableOrderQueue
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
	toggles.SetAutomaticDiscoveryPrioritySink(func(enabled bool) {
		queue.SetAutomaticDiscoveryPriority(enabled)
		control.SetAutomaticDiscoveryPriority(enabled)
	})
}

func (r *crawlRuntime) automaticDiscoveryQueue() *crawlbroker.DurableOrderQueue {
	return r.broker.Orders
}
