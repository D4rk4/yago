package yagonode

import "context"

type indexQueueDepthSource struct {
	probe func() (int, bool)
}

func (s indexQueueDepthSource) observation(context.Context) (int, bool) {
	if s.probe == nil {
		return 0, true
	}

	depth, known := s.probe()

	return max(0, depth), known
}

func (r *crawlRuntime) indexQueueDepth() (int, bool) {
	if r.exchange == nil || r.exchange.Ingest == nil {
		return 0, false
	}
	return r.exchange.Ingest.Outstanding(), true
}

func indexQueueProbe(runtime crawlProcess) func() (int, bool) {
	probe, ok := runtime.(interface {
		indexQueueDepth() (int, bool)
	})
	if !ok {
		return nil
	}

	return probe.indexQueueDepth
}
