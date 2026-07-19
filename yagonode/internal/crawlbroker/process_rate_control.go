package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

func (r *ControlRegistry) ProcessPagesPerSecond() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return int(r.processPagesPerSecond)
}

func (r *ControlRegistry) SetProcessPagesPerSecond(pagesPerSecond int) int {
	if pagesPerSecond < 0 || pagesPerSecond > yagocrawlcontract.MaximumProcessPagesPerSecond {
		return 0
	}
	r.processRateUpdate.Lock()
	defer r.processRateUpdate.Unlock()
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
		ProcessPagesPerSecond: uint32(pagesPerSecond),
	}
	r.mu.Lock()
	setFleetPagesPerSecond := r.setFleetPagesPerSecond
	r.mu.Unlock()
	if setFleetPagesPerSecond != nil {
		if err := setFleetPagesPerSecond(uint32(pagesPerSecond)); err != nil {
			return 0
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processPagesPerSecond = uint32(pagesPerSecond)
	r.processRateSet = true
	signalled := 0
	for workerID := range r.workers {
		if r.enqueueLocked(workerID, directive) {
			signalled++
		} else {
			r.initialized[workerID] = false
		}
	}

	return signalled
}
