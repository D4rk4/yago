package yagonode

import "context"

// queueDepthSource adapts the DHT gate snapshot to the metrics collector, so the
// crawl and index queue depths are scraped straight from the live gate state.
type queueDepthSource struct {
	gates dhtGateStatusSource
}

func newQueueDepthSource(gates dhtGateStatusSource) queueDepthSource {
	return queueDepthSource{gates: gates}
}

func (s queueDepthSource) CrawlQueueDepth(ctx context.Context) int {
	return s.gates.response(ctx).State.CrawlQueueSize
}

func (s queueDepthSource) IndexQueueDepth(ctx context.Context) int {
	return s.gates.response(ctx).State.IndexQueueSize
}
