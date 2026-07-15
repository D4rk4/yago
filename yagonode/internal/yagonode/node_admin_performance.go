package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

type performanceSource struct {
	gates dhtGateStatusSource
}

func newPerformanceSource(gates dhtGateStatusSource) performanceSource {
	return performanceSource{gates: gates}
}

func (s performanceSource) Performance(ctx context.Context) adminui.PerformanceStatus {
	state := s.gates.response(ctx).State

	return adminui.PerformanceStatus{
		Available:        s.gates.snapshot != nil,
		CrawlQueueSize:   state.CrawlQueueSize,
		CrawlQueueKnown:  state.CrawlQueueKnown,
		IndexQueueSize:   state.IndexQueueSize,
		IndexQueueKnown:  state.IndexQueueKnown,
		ConnectedPeers:   state.ConnectedPeers,
		LocalRWIWords:    state.LocalRWIWords,
		LocalRWIKnown:    state.LocalRWIKnown,
		StorageAvailable: state.StorageAvailable,
		StorageKnown:     state.StorageKnown,
	}
}
