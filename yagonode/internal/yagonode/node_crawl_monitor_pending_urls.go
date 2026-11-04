package yagonode

import (
	"context"
)

func (r *crawlRuntime) pendingOrderURLs(ctx context.Context) ([]string, error) {
	return r.broker.Orders.PendingURLSample(ctx)
}

func crawlQueuePendingURLs(runtime crawlProcess) func(context.Context) ([]string, error) {
	provider, ok := runtime.(interface {
		pendingOrderURLs(context.Context) ([]string, error)
	})
	if !ok {
		return nil
	}

	return provider.pendingOrderURLs
}

func (s *crawlMonitorSource) withPendingURLs(
	sample func(context.Context) ([]string, error),
) *crawlMonitorSource {
	s.pendingURLs = sample

	return s
}
