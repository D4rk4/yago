package crawlbroker

import "time"

type leaseDeferral struct {
	leaseID         string
	workerID        string
	workerSessionID string
	requireOwner    bool
	retryAfter      time.Duration
}
