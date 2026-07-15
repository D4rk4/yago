package yagonode

import "context"

// queueDepthSource feeds the metrics collector: the crawl queue depth comes from
// the broker's order backlog, and the index queue depth from the live gate state.
type queueDepthSource struct {
	gates dhtGateStatusSource
	crawl crawlQueueDepthSource
}

func newQueueDepthSource(gates dhtGateStatusSource, crawl crawlQueueDepthSource) queueDepthSource {
	return queueDepthSource{gates: gates, crawl: crawl}
}

func (s queueDepthSource) CrawlQueueDepth(ctx context.Context) (int, bool) {
	return s.crawl.observation(ctx)
}

func (s queueDepthSource) IndexQueueDepth(ctx context.Context) (int, bool) {
	state := s.gates.response(ctx).State

	return state.IndexQueueSize, state.IndexQueueKnown
}
