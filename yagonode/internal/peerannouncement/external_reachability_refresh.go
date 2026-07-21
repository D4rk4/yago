package peerannouncement

import "time"

const externalReachabilityRefreshInterval = 10 * time.Minute

func (a *announcer) tickSource(interval time.Duration) (<-chan time.Time, func()) {
	if a.ticks != nil {
		return a.ticks(interval)
	}
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func (a *announcer) externalRefreshTicks() (<-chan time.Time, func()) {
	interval := a.externalRefreshInterval
	if interval <= 0 {
		interval = externalReachabilityRefreshInterval
	}
	if a.interval <= interval {
		return nil, func() {}
	}

	return a.tickSource(interval)
}
