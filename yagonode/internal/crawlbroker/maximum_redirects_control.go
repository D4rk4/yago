package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

func (r *ControlRegistry) MaximumRedirects() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return int(r.maximumRedirects)
}

func (r *ControlRegistry) SetMaximumRedirects(maximum int) int {
	if maximum < 0 || maximum > yagocrawlcontract.MaximumPageRedirects {
		return 0
	}
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
		MaximumRedirects: uint32(maximum),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maximumRedirects = uint32(maximum)
	r.maximumRedirectsSet = true
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
