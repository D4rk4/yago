package main

import (
	"context"
	"expvar"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
)

const metricStorageBytes = "storage_bytes"

func publishStorageMetrics(vault *boltvault.Vault) {
	if expvar.Get(metricStorageBytes) != nil {
		return
	}

	expvar.Publish(metricStorageBytes, expvar.Func(func() any {
		used, err := vault.UsedBytes(context.Background())
		if err != nil {
			used = 0
		}

		return map[string]int64{
			"quota": vault.QuotaBytes(),
			"used":  used,
		}
	}))
}
