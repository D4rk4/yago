package yagonode

import (
	"fmt"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func attachCrawlStateMetrics(runtime crawlProcess, registry prometheus.Registerer) {
	state, ok := runtime.(*crawlRuntime)
	if !ok || registry == nil || !state.ownsState || state.state == nil || state.statePath == "" {
		return
	}
	metrics.NewCrawlStateMetrics(registry, state.state, func() (int64, error) {
		info, err := os.Stat(state.statePath)
		if err != nil {
			return 0, fmt.Errorf("stat crawl runtime state: %w", err)
		}

		return info.Size(), nil
	})
}
