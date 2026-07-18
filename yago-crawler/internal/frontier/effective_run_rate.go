package frontier

func (f *Frontier) EffectivePagesPerMinute(provenance []byte) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()

	if pagesPerMinute, explicit := f.pagesPerMinute[string(provenance)]; explicit {
		return pagesPerMinute
	}

	return f.defaultPagesPerMinute
}
