package main

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func newCrawlerStorageAdmission(
	cfg ServiceConfig,
	metrics *crawlermetrics.Metrics,
) *yagocrawlcontract.StoragePressureGate {
	admission := yagocrawlcontract.NewStoragePressureGate(
		cfg.DataDir,
		yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes:       cfg.StorageReservedFreeBytes,
			RecoveryHysteresisBytes: cfg.StoragePressureHysteresisBytes,
		},
	)
	metrics.RegisterStoragePressure(admission)

	return admission
}
