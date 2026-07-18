package yagonode

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func newNodeStoragePressure(
	config nodeConfig,
	toggles *runtimeToggles,
	endpoints *metrics.HTTPEndpointMetrics,
) *yagocrawlcontract.StoragePressureGate {
	pressure := yagocrawlcontract.NewStoragePressureGate(
		config.DataDir,
		nodeStoragePressurePolicy(config),
	)
	toggles.SetStoragePressureSink(pressure.SetPolicy)
	metrics.NewStoragePressureMetrics(endpoints.Registry(), pressure)

	return pressure
}

func (t *runtimeToggles) SetStoragePressureSink(
	sink func(yagocrawlcontract.StoragePressurePolicy),
) {
	if t != nil && sink != nil {
		t.storagePressurePolicy.Store(sink)
	}
}

func (t *runtimeToggles) ApplyStorageReservedFree(bytes int64) {
	if t == nil {
		return
	}
	t.storageReservedFree.Store(bytes)
	t.applyStoragePressurePolicy()
}

func (t *runtimeToggles) ApplyStoragePressureRecovery(bytes int64) {
	if t == nil {
		return
	}
	t.storagePressureRecovery.Store(bytes)
	t.applyStoragePressurePolicy()
}

func (t *runtimeToggles) applyStoragePressurePolicy() {
	if sink, ok := t.storagePressurePolicy.Load().(func(
		yagocrawlcontract.StoragePressurePolicy,
	)); ok {
		sink(storagePressurePolicy(
			t.storageReservedFree.Load(),
			t.storagePressureRecovery.Load(),
		))
	}
}

func (t *runtimeToggles) SetCrawlerStoragePressureSink(
	sink func(yagocrawlcontract.StoragePressurePolicy),
) {
	if t != nil && sink != nil {
		t.crawlerStoragePolicy.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerStorageReservedFree(bytes int64) {
	if t == nil {
		return
	}
	t.crawlerStorageReservedFree.Store(bytes)
	t.applyCrawlerStoragePressurePolicy()
}

func (t *runtimeToggles) ApplyCrawlerStoragePressureRecovery(bytes int64) {
	if t == nil {
		return
	}
	t.crawlerStorageRecovery.Store(bytes)
	t.applyCrawlerStoragePressurePolicy()
}

func (t *runtimeToggles) applyCrawlerStoragePressurePolicy() {
	if sink, ok := t.crawlerStoragePolicy.Load().(func(
		yagocrawlcontract.StoragePressurePolicy,
	)); ok {
		sink(storagePressurePolicy(
			t.crawlerStorageReservedFree.Load(),
			t.crawlerStorageRecovery.Load(),
		))
	}
}
