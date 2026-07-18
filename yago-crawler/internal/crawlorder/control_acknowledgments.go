package crawlorder

import (
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type controlAcknowledgments struct {
	mu      sync.Mutex
	pending []uint64
}

func (a *controlAcknowledgments) snapshot() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	limit := min(len(a.pending), yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments)

	return append([]uint64(nil), a.pending[:limit]...)
}

func (a *controlAcknowledgments) confirm(acknowledged []uint64) {
	if len(acknowledged) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	confirmed := make(map[uint64]struct{}, len(acknowledged))
	for _, directiveID := range acknowledged {
		confirmed[directiveID] = struct{}{}
	}
	retained := a.pending[:0]
	for _, directiveID := range a.pending {
		if _, found := confirmed[directiveID]; !found {
			retained = append(retained, directiveID)
		}
	}
	a.pending = retained
}

func (a *controlAcknowledgments) add(directiveIDs []uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, directiveID := range directiveIDs {
		if len(a.pending) == yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
			return
		}
		present := false
		for _, pendingID := range a.pending {
			if pendingID == directiveID {
				present = true

				break
			}
		}
		if !present {
			a.pending = append(a.pending, directiveID)
		}
	}
}

func (a *controlAcknowledgments) available() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments - len(a.pending)
}
