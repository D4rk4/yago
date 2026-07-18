package searchremote

import (
	"sync/atomic"
)

const (
	remoteQueryPeerCallBudget      = 32
	remoteMorphologyPeerCallBudget = 20
	remoteMorphologyConcurrency    = 8
)

var remoteMorphologySearchAdmission = make(chan struct{}, remoteMorphologyConcurrency)

type outboundCallBudget struct {
	remaining atomic.Int32
}

func newOutboundCallBudget(limit int32) *outboundCallBudget {
	budget := &outboundCallBudget{}
	budget.remaining.Store(limit)

	return budget
}

func (budget *outboundCallBudget) available() int {
	if budget == nil {
		return 0
	}

	available := 0
	for remaining := budget.remaining.Load(); remaining > 0; remaining-- {
		available++
	}

	return available
}

func (budget *outboundCallBudget) acquire() bool {
	if budget == nil {
		return true
	}
	for {
		remaining := budget.remaining.Load()
		if remaining <= 0 {
			return false
		}
		if budget.remaining.CompareAndSwap(remaining, remaining-1) {
			return true
		}
	}
}

func (budget *outboundCallBudget) restore() {
	if budget != nil {
		budget.remaining.Add(1)
	}
}

func acquireOutboundCall(budgets ...*outboundCallBudget) bool {
	acquired := make([]*outboundCallBudget, 0, len(budgets))
	for _, budget := range budgets {
		if budget == nil {
			continue
		}
		if !budget.acquire() {
			for _, prior := range acquired {
				prior.restore()
			}

			return false
		}
		acquired = append(acquired, budget)
	}

	return true
}

func peerJobsWithinCallBudget(
	requests []peerSearchJob,
	budget *remoteQueryBudget,
) []peerSearchJob {
	if budget == nil || budget.peerCalls == nil {
		return nil
	}
	maximum := min(len(requests), budget.peerCalls.available())
	limited := make([]peerSearchJob, 0, maximum)
	morphologyMaximum := 0
	if budget.morphologyCalls != nil {
		morphologyMaximum = budget.morphologyCalls.available()
	}
	morphologyPlanned := 0
	for _, request := range requests {
		if len(limited) == maximum {
			break
		}
		request.peerCalls = budget.peerCalls
		if request.morphology {
			if morphologyPlanned == morphologyMaximum {
				continue
			}
			request.morphologyCalls = budget.morphologyCalls
			morphologyPlanned++
		}
		limited = append(limited, request)
	}

	return limited
}

func (s searcher) morphologySearchAdmission() chan struct{} {
	if s.morphologyAdmission != nil {
		return s.morphologyAdmission
	}

	return remoteMorphologySearchAdmission
}
