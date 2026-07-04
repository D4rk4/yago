package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

// performanceSource adapts the DHT gate snapshot to the console's Performance
// section, surfacing queue depths, connected-peer count and storage headroom. The
// crawl queue size comes from the broker backlog rather than the gate state.
type performanceSource struct {
	gates dhtGateStatusSource
	crawl crawlQueueDepthSource
}

func newPerformanceSource(
	gates dhtGateStatusSource,
	crawl crawlQueueDepthSource,
) performanceSource {
	return performanceSource{gates: gates, crawl: crawl}
}

func (s performanceSource) Performance(ctx context.Context) adminui.PerformanceStatus {
	state := s.gates.response(ctx).State

	return adminui.PerformanceStatus{
		Available:        true,
		CrawlQueueSize:   s.crawl.outstanding(ctx),
		IndexQueueSize:   state.IndexQueueSize,
		ConnectedPeers:   state.ConnectedPeers,
		LocalRWIWords:    state.LocalRWIWords,
		StorageAvailable: state.StorageAvailable,
	}
}
