package main

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const labelStorageKind = "kind"

func publishStorageMetrics(registry prometheus.Registerer, vault *boltvault.Vault) {
	registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "storage_quota_bytes",
			Help:        "Configured storage quota in bytes.",
			ConstLabels: prometheus.Labels{labelStorageKind: "quota"},
		},
		func() float64 { return float64(vault.QuotaBytes()) },
	))
	registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "storage_used_bytes",
			Help:        "Storage currently used in bytes.",
			ConstLabels: prometheus.Labels{labelStorageKind: "used"},
		},
		func() float64 {
			used, err := vault.UsedBytes(context.Background())
			if err != nil {
				return 0
			}

			return float64(used)
		},
	))
}
