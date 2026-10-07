package searchremote

import (
	"sync/atomic"

	"github.com/D4rk4/yago/yagomodel"
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
) (admitted []peerSearchJob, skipped int, skippedTerms map[yagomodel.Hash]struct{}) {
	if budget == nil || budget.peerCalls == nil {
		return nil, 0, nil
	}
	maximum := min(len(requests), budget.peerCalls.available())
	admitted = make([]peerSearchJob, 0, maximum)
	morphologyMaximum := 0
	if budget.morphologyCalls != nil {
		morphologyMaximum = budget.morphologyCalls.available()
	}
	morphologyPlanned := 0
	for position, request := range requests {
		if len(admitted) == maximum {
			skipped += len(requests[position:])
			skippedTerms = addOmittedPeerTerms(skippedTerms, requests[position:])
			break
		}
		request, allowed := peerSearchJobWithinCallBudget(
			request,
			budget,
			morphologyMaximum,
			&morphologyPlanned,
		)
		if !allowed {
			skipped++
			skippedTerms = addOmittedPeerTerms(skippedTerms, []peerSearchJob{request})
			continue
		}
		admitted = append(admitted, request)
	}

	return admitted, skipped, skippedTerms
}

func peerSearchJobWithinCallBudget(
	request peerSearchJob,
	budget *remoteQueryBudget,
	morphologyMaximum int,
	morphologyPlanned *int,
) (peerSearchJob, bool) {
	request.peerCalls = budget.peerCalls
	request.transportAttempts = budget.transportAttempts
	if !request.morphology {
		return request, true
	}
	if *morphologyPlanned >= morphologyMaximum {
		return request, false
	}
	request.morphologyCalls = budget.morphologyCalls
	*morphologyPlanned++

	return request, true
}

func addOmittedPeerTerms(
	terms map[yagomodel.Hash]struct{},
	requests []peerSearchJob,
) map[yagomodel.Hash]struct{} {
	for _, request := range requests {
		if request.term == "" {
			continue
		}
		if terms == nil {
			terms = make(map[yagomodel.Hash]struct{})
		}
		terms[request.term] = struct{}{}
	}

	return terms
}

func (s searcher) morphologySearchAdmission() chan struct{} {
	if s.morphologyAdmission != nil {
		return s.morphologyAdmission
	}

	return remoteMorphologySearchAdmission
}
