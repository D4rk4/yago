package crawlorder

import "sync"

const recentCompletedLeaseCapacity = 4096

type activeOrders struct {
	mu                     sync.Mutex
	deliveries             map[string]activeOrderDelivery
	completedLeases        map[string]struct{}
	completedLeaseOrder    []string
	nextCompletedLeaseSlot int
}

func newActiveOrders() *activeOrders {
	return &activeOrders{
		deliveries:      make(map[string]activeOrderDelivery),
		completedLeases: make(map[string]struct{}),
	}
}

type activeOrderDelivery struct {
	delivery CrawlOrderDelivery
	version  uint64
}

type activeOrderClaim uint8

const (
	activeOrderStartsRun activeOrderClaim = iota
	activeOrderJoinsRun
	activeOrderAlreadyCompleted
)

func (a *activeOrders) claim(
	provenance []byte,
	delivery CrawlOrderDelivery,
) activeOrderClaim {
	a.mu.Lock()
	defer a.mu.Unlock()
	if delivery.LeaseID != "" {
		if _, completed := a.completedLeases[delivery.LeaseID]; completed {
			return activeOrderAlreadyCompleted
		}
	}
	key := activeOrderIdentity(provenance, delivery.LeaseID)
	if key == "" {
		return activeOrderStartsRun
	}
	if current, found := a.deliveries[key]; found {
		if delivery.LeaseID == "" || delivery.LeaseID != current.delivery.LeaseID {
			current.version++
		}
		current.delivery = delivery
		a.deliveries[key] = current

		return activeOrderJoinsRun
	}
	a.deliveries[key] = activeOrderDelivery{delivery: delivery, version: 1}

	return activeOrderStartsRun
}

func (a *activeOrders) settle(
	provenance []byte,
	fallback CrawlOrderDelivery,
	retainCompletion bool,
	settle func(CrawlOrderDelivery),
) {
	key := activeOrderIdentity(provenance, fallback.LeaseID)
	if key == "" {
		settle(fallback)
		if retainCompletion {
			a.mu.Lock()
			a.rememberCompletedLease(fallback.LeaseID)
			a.mu.Unlock()
		}

		return
	}
	for {
		a.mu.Lock()
		current, found := a.deliveries[key]
		if !found {
			a.mu.Unlock()

			return
		}
		if retainCompletion {
			a.rememberCompletedLease(current.delivery.LeaseID)
		}
		a.mu.Unlock()
		settle(current.delivery)

		a.mu.Lock()
		latest, found := a.deliveries[key]
		if found && latest.version == current.version {
			delete(a.deliveries, key)
			a.mu.Unlock()

			return
		}
		a.mu.Unlock()
	}
}

func activeOrderIdentity(provenance []byte, leaseID string) string {
	if len(provenance) != 0 {
		return "provenance\x00" + string(provenance)
	}
	if leaseID != "" {
		return "lease\x00" + leaseID
	}

	return ""
}

func (a *activeOrders) rememberCompletedLease(leaseID string) {
	if leaseID == "" {
		return
	}
	if _, exists := a.completedLeases[leaseID]; exists {
		return
	}
	if len(a.completedLeaseOrder) < recentCompletedLeaseCapacity {
		a.completedLeaseOrder = append(a.completedLeaseOrder, leaseID)
	} else {
		delete(a.completedLeases, a.completedLeaseOrder[a.nextCompletedLeaseSlot])
		a.completedLeaseOrder[a.nextCompletedLeaseSlot] = leaseID
		a.nextCompletedLeaseSlot = (a.nextCompletedLeaseSlot + 1) % recentCompletedLeaseCapacity
	}
	a.completedLeases[leaseID] = struct{}{}
}

func (a *activeOrders) provenances() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := make(map[string]struct{}, len(a.deliveries))
	provenances := make([][]byte, 0, len(a.deliveries))
	for _, current := range a.deliveries {
		provenance := current.delivery.Order.Provenance
		key := string(provenance)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		provenances = append(provenances, append([]byte(nil), provenance...))
	}

	return provenances
}
