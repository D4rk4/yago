package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

type crawlerControlDefaults struct {
	fetchWorkers                 uint32
	prioritizeAutomaticDiscovery bool
}

func (r *ControlRegistry) SetAutomaticDiscoveryPriority(enabled bool) int {
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:                         yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority,
		PrioritizeAutomaticDiscovery: enabled,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prioritizeAutomaticDiscovery = enabled
	r.automaticDiscoveryPrioritySet = true
	for workerID := range r.workers {
		r.pending[workerID] = append(r.pending[workerID], directive)
	}

	return len(r.workers)
}

func (r *ControlRegistry) initialDirectivesLocked() []yagocrawlcontract.CrawlControlDirective {
	directives := make([]yagocrawlcontract.CrawlControlDirective, 0, 2)
	if r.fetchWorkersSet {
		directives = append(directives, yagocrawlcontract.CrawlControlDirective{
			Kind:         yagocrawlcontract.CrawlControlSetWorkers,
			FetchWorkers: r.fetchWorkers,
		})
	}
	if r.automaticDiscoveryPrioritySet {
		directives = append(directives, yagocrawlcontract.CrawlControlDirective{
			Kind:                         yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority,
			PrioritizeAutomaticDiscovery: r.prioritizeAutomaticDiscovery,
		})
	}

	return directives
}

func (r *ControlRegistry) drainForHeartbeat(
	workerID string,
) []yagocrawlcontract.CrawlControlDirective {
	r.mu.Lock()
	defer r.mu.Unlock()

	directives := r.pending[workerID]
	delete(r.pending, workerID)
	if r.workers[workerID] == 0 {
		directives = append(directives, r.initialDirectivesLocked()...)
	}

	return directives
}
