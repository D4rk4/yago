package frontier

func (f *Frontier) Suspend(provenance []byte) {
	key := string(provenance)

	f.mu.Lock()
	f.suspended[key] = struct{}{}
	finishes := f.cancelQueuedLocked(key)
	f.mu.Unlock()

	f.scheduleSettlements(finishes)
	f.wake()
}

func (f *Frontier) WasSuspended(provenance []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, suspended := f.suspended[string(provenance)]

	return suspended
}

func (f *Frontier) ClearSuspended(provenance []byte) {
	f.mu.Lock()
	key := string(provenance)
	delete(f.suspended, key)
	delete(f.deferredUntil, key)
	f.mu.Unlock()
}
