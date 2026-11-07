package yagonode

import (
	"context"
	"fmt"
)

func (r *crawlRuntime) pendingOrderURLs(ctx context.Context) ([]string, error) {
	urls, err := r.broker.Orders.PendingURLSample(ctx)
	if err != nil {
		return nil, fmt.Errorf("sample pending crawl URLs: %w", err)
	}

	return urls, nil
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
