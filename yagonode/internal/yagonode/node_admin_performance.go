package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

// performanceSource adapts the DHT gate snapshot to the console's Performance
// section, surfacing queue depths, connected-peer count and storage headroom.
type performanceSource struct {
	gates dhtGateStatusSource
}

func newPerformanceSource(gates dhtGateStatusSource) performanceSource {
	return performanceSource{gates: gates}
}

func (s performanceSource) Performance(ctx context.Context) adminui.PerformanceStatus {
	state := s.gates.response(ctx).State

	return adminui.PerformanceStatus{
		Available:        true,
		CrawlQueueSize:   state.CrawlQueueSize,
		IndexQueueSize:   state.IndexQueueSize,
		ConnectedPeers:   state.ConnectedPeers,
		LocalRWIWords:    state.LocalRWIWords,
		StorageAvailable: state.StorageAvailable,
	}
}
