package crawllease

func (r *GrantRegistry) LeaseLosses() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())

	return r.leaseLosses
}

func (r *GrantRegistry) Reject(leaseID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.grants[leaseID]
	if !exists {
		return
	}
	r.loseLocked(leaseID, current)
	r.signalLocked()
	r.signalAvailabilityChangeLocked()
	r.signalLeaseLossLocked()
}
