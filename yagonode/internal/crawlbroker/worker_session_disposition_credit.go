package crawlbroker

func (r *workerSessionRegistry) confirmDisposition(
	workerID string,
	workerSessionID string,
	leaseID string,
) {
	entry, err := r.retain(workerID, false)
	if err != nil {
		return
	}
	defer r.releaseRetention(entry)
	entry.mutex.Lock()
	current := entry.current
	if current.id != workerSessionID || !current.connected || current.deliveryCredit == nil {
		entry.mutex.Unlock()

		return
	}
	credit := current.deliveryCredit
	entry.mutex.Unlock()
	credit.confirm([]string{leaseID})
}
