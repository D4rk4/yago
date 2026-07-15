package adminui

import "context"

// PerformanceStatus is the operational snapshot the Performance section renders.
type PerformanceStatus struct {
	Available        bool
	CrawlQueueSize   int
	CrawlQueueKnown  bool
	IndexQueueSize   int
	IndexQueueKnown  bool
	ConnectedPeers   int
	LocalRWIWords    int
	LocalRWIKnown    bool
	StorageAvailable bool
	StorageKnown     bool
}

// PerformanceSource supplies the operational snapshot on each request.
type PerformanceSource interface {
	Performance(ctx context.Context) PerformanceStatus
}
