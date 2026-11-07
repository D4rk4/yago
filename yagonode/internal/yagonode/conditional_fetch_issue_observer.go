package yagonode

import "github.com/D4rk4/yago/yagocrawlcontract"

type conditionalFetchIssueObserver interface {
	ObserveConditionalFetchIssued()
}

func (s recrawlSweeper) observePublishedConditionalFetch(
	conditionalFetch *yagocrawlcontract.ConditionalFetch,
) {
	if conditionalFetch != nil && s.conditionalFetchIssues != nil {
		s.conditionalFetchIssues.ObserveConditionalFetchIssued()
	}
}
