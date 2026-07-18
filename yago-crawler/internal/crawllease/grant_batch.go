package crawllease

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (r *GrantRegistry) TrackMany(leaseIDs []string) ([]string, error) {
	unique := make([]string, 0, len(leaseIDs))
	seen := make(map[string]struct{}, len(leaseIDs))
	for _, leaseID := range leaseIDs {
		if !yagocrawlcontract.ValidCrawlLeaseID(leaseID) {
			return nil, fmt.Errorf("track crawl lease: invalid lease id")
		}
		if _, duplicate := seen[leaseID]; duplicate {
			continue
		}
		seen[leaseID] = struct{}{}
		unique = append(unique, leaseID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	added := make([]string, 0, len(unique))
	for _, leaseID := range unique {
		if _, exists := r.grants[leaseID]; !exists {
			added = append(added, leaseID)
		}
	}
	if len(r.grants)+len(added) > r.capacity {
		return nil, fmt.Errorf("track crawl lease: capacity %d reached", r.capacity)
	}
	for _, leaseID := range added {
		ctx, cancel := context.WithCancelCause(context.Background())
		r.grants[leaseID] = &grant{ctx: ctx, cancel: cancel}
	}
	if len(added) > 0 {
		r.signalLocked()
		r.signalAvailabilityChangeLocked()
	}

	return added, nil
}
