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
	delivery   CrawlOrderDelivery
	definition activeOrderDefinition
	version    uint64
	settling   bool
}

type activeOrderClaim uint8

const (
	activeOrderStartsRun activeOrderClaim = iota
	activeOrderJoinsRun
	activeOrderRecoversCompletedRun
	activeOrderAlreadyCompleted
	activeOrderRejected
)

func (a *activeOrders) claim(
	provenance []byte,
	delivery CrawlOrderDelivery,
	leaseRebinders ...activeOrderLeaseRebinder,
) activeOrderClaim {
	definition, valid := identifyActiveOrder(delivery)
	if !valid {
		return activeOrderRejected
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.leaseCompleted(delivery.LeaseID) {
		return activeOrderAlreadyCompleted
	}
	key := activeOrderIdentity(provenance, delivery.LeaseID)
	if key == "" {
		return activeOrderStartsRun
	}
	if current, found := a.deliveries[key]; found {
		return a.claimExisting(key, current, definition, delivery, leaseRebinders)
	}
	a.deliveries[key] = activeOrderDelivery{
		delivery:   delivery,
		definition: definition,
		version:    1,
	}

	return activeOrderStartsRun
}

func (a *activeOrders) leaseCompleted(leaseID string) bool {
	if leaseID == "" {
		return false
	}
	_, completed := a.completedLeases[leaseID]

	return completed
}

func (a *activeOrders) claimExisting(
	key string,
	current activeOrderDelivery,
	definition activeOrderDefinition,
	delivery CrawlOrderDelivery,
	leaseRebinders []activeOrderLeaseRebinder,
) activeOrderClaim {
	if !current.definition.matches(definition) {
		return activeOrderRejected
	}
	claim := a.rebindClaim(current, delivery, leaseRebinders)
	if claim != activeOrderJoinsRun {
		if claim == activeOrderRecoversCompletedRun {
			current.version++
			current.delivery = delivery
			current.settling = false
			a.deliveries[key] = current
		}

		return claim
	}
	if current.settling || delivery.LeaseID == "" ||
		delivery.LeaseID != current.delivery.LeaseID {
		current.version++
	}
	current.delivery = delivery
	current.settling = false
	a.deliveries[key] = current

	return activeOrderJoinsRun
}

func (a *activeOrders) rebindClaim(
	current activeOrderDelivery,
	delivery CrawlOrderDelivery,
	leaseRebinders []activeOrderLeaseRebinder,
) activeOrderClaim {
	if delivery.LeaseID == "" || delivery.LeaseID == current.delivery.LeaseID ||
		len(leaseRebinders) == 0 {
		return activeOrderJoinsRun
	}

	return leaseRebinders[0](current.delivery.LeaseID, delivery.LeaseID)
}

func (a *activeOrders) settle(
	provenance []byte,
	fallback CrawlOrderDelivery,
	retainCompletion bool,
	settle func(CrawlOrderDelivery) bool,
) bool {
	return a.settleWithFailureRetention(
		provenance,
		fallback,
		retainCompletion,
		true,
		settle,
	)
}

func (a *activeOrders) settleDurably(
	provenance []byte,
	fallback CrawlOrderDelivery,
	retainCompletion bool,
	settle func(CrawlOrderDelivery) bool,
) bool {
	return a.settleWithFailureRetention(
		provenance,
		fallback,
		retainCompletion,
		false,
		settle,
	)
}

func (a *activeOrders) settleWithFailureRetention(
	provenance []byte,
	fallback CrawlOrderDelivery,
	retainCompletion bool,
	rememberFailed bool,
	settle func(CrawlOrderDelivery) bool,
) bool {
	key := activeOrderIdentity(provenance, fallback.LeaseID)
	if key == "" {
		succeeded := settle(fallback)
		if retainCompletion && (succeeded || rememberFailed) {
			a.mu.Lock()
			a.rememberCompletedLease(fallback.LeaseID)
			a.mu.Unlock()
		}

		return succeeded
	}
	for {
		a.mu.Lock()
		current, found := a.deliveries[key]
		if !found {
			a.mu.Unlock()

			return false
		}
		if retainCompletion && rememberFailed {
			a.rememberCompletedLease(current.delivery.LeaseID)
		}
		current.settling = true
		a.deliveries[key] = current
		a.mu.Unlock()
		succeeded := settle(current.delivery)

		a.mu.Lock()
		latest, found := a.deliveries[key]
		if found && latest.version == current.version {
			delete(a.deliveries, key)
			if succeeded && retainCompletion && !rememberFailed {
				a.rememberCompletedLease(current.delivery.LeaseID)
			}
			a.mu.Unlock()

			return succeeded
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
