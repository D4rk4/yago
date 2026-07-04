package adminui

import "context"

// PerformanceStatus is the operational snapshot the Performance section renders.
type PerformanceStatus struct {
	Available        bool
	CrawlQueueSize   int
	IndexQueueSize   int
	ConnectedPeers   int
	LocalRWIWords    int
	StorageAvailable bool
}

// PerformanceSource supplies the operational snapshot on each request.
type PerformanceSource interface {
	Performance(ctx context.Context) PerformanceStatus
}
