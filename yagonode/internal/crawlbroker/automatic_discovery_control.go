package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlerControlDefaults struct {
	fetchWorkers                 uint32
	maximumActiveRuns            uint32
	prioritizeAutomaticDiscovery bool
	storagePressurePolicy        yagocrawlcontract.StoragePressurePolicy
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

func (r *ControlRegistry) initialDirectivesLocked() []yagocrawlcontract.CrawlControlDirective {
	directives := make([]yagocrawlcontract.CrawlControlDirective, 0, 3)
	if r.fetchWorkersSet {
		directives = append(directives, yagocrawlcontract.CrawlControlDirective{
			Kind:         yagocrawlcontract.CrawlControlSetWorkers,
			FetchWorkers: r.fetchWorkers,
		})
	}
	if r.maximumActiveRunsSet {
		directives = append(directives, yagocrawlcontract.CrawlControlDirective{
			Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
			MaximumActiveRuns: r.maximumActiveRuns,
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

func (r *ControlRegistry) ensureInitialLocked(
	ctx context.Context,
	workerID string,
) error {
	pending, err := r.directives.Exchange(ctx, workerID, nil)
	if err != nil {
		return fmt.Errorf("read initial crawl control directives: %w", err)
	}
	for _, wanted := range r.initialDirectivesLocked() {
		present := false
		for _, existing := range pending {
			existing.DirectiveID = 0
			if existing == wanted {
				present = true

				break
			}
		}
		if present {
			continue
		}
		if _, err := r.directives.Enqueue(ctx, workerID, wanted); err != nil {
			return fmt.Errorf("enqueue initial crawl control directive: %w", err)
		}
	}

	return nil
}

func (r *ControlRegistry) deliverForHeartbeat(
	ctx context.Context,
	workerID string,
	acknowledged []uint64,
) ([]yagocrawlcontract.CrawlControlDirective, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.initialized[workerID] {
		if err := r.ensureInitialLocked(ctx, workerID); err != nil {
			return nil, err
		}
		r.initialized[workerID] = true
	}

	directives, err := r.directives.Exchange(ctx, workerID, acknowledged)
	if err != nil {
		return nil, fmt.Errorf("exchange crawl control directives: %w", err)
	}

	return directives, nil
}
