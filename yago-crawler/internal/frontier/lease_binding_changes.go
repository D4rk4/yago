package frontier

func (f *Frontier) LeaseBindingChanges() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.leaseBindingChanges
}

func (f *Frontier) signalLeaseBindingChangeLocked() {
	close(f.leaseBindingChanges)
	f.leaseBindingChanges = make(chan struct{})
}
