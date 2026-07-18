package crawllease

import "time"

type grantSettlement struct {
	attempts         int
	responseAt       time.Time
	responseRejected bool
}

func (settlement *grantSettlement) active() bool {
	return settlement.attempts > 0
}

func (settlement *grantSettlement) recordResponse(started time.Time, live bool) {
	settlement.responseAt = started
	settlement.responseRejected = !live
}

func (r *GrantRegistry) BeginSettlement(leaseID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())
	current, exists := r.grants[leaseID]
	if !exists || !current.confirmed {
		return false
	}
	if !current.settling.active() {
		current.settling.responseAt = time.Time{}
		current.settling.responseRejected = false
	}
	current.settling.attempts++
	r.signalLocked()

	return true
}

func (r *GrantRegistry) SettlementFailed(leaseID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.grants[leaseID]
	if !exists || !current.settling.active() {
		return
	}
	current.settling.attempts--
	if current.settling.active() {
		return
	}
	rejected := current.settling.responseRejected
	current.settling.responseAt = time.Time{}
	current.settling.responseRejected = false
	r.signalLocked()
	if !rejected && (!current.confirmed || current.expiresAt.After(r.now())) {
		return
	}
	r.loseLocked(leaseID, current)
	r.signalAvailabilityChangeLocked()
	r.signalLeaseLossLocked()
}

func (r *GrantRegistry) Settle(leaseID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.grants[leaseID]
	if !exists {
		return
	}
	current.cancel(nil)
	delete(r.grants, leaseID)
	r.signalLocked()
	r.signalAvailabilityChangeLocked()
}
