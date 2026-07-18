package crawlruns

func (r *Registry) reconcileRunActivityLocked(existed bool, previous Run, current Run) {
	wasActive := existed && !isTerminal(previous.State)
	isActive := !isTerminal(current.State)
	switch {
	case !wasActive && isActive:
		r.activeRuns++
	case wasActive && !isActive:
		r.activeRuns--
	}
}

func (r *Registry) evictLocked() {
	for len(r.runs)-r.activeRuns > r.capacity {
		oldestID := ""
		oldest := Run{}
		for runID, run := range r.runs {
			if !isTerminal(run.State) {
				continue
			}
			if oldestID == "" || run.Updated.Before(oldest.Updated) ||
				run.Updated.Equal(oldest.Updated) && runID < oldestID {
				oldestID = runID
				oldest = run
			}
		}
		delete(r.runs, oldestID)
	}
}
