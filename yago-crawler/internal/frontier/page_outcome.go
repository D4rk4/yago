package frontier

import (
	"errors"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type OutcomeCount struct {
	Class  string
	Reason string
	Pages  uint64
}

type PageOutcome struct {
	Class  string
	Reason string
}

const (
	OutcomeDocumentCandidate = yagocrawlcontract.CrawlOutcomeDocumentCandidate
	OutcomeRemovalMutation   = yagocrawlcontract.CrawlOutcomeRemovalMutation
	OutcomeNotIndexable      = yagocrawlcontract.CrawlOutcomeNotIndexable
	OutcomeNodeRejected      = yagocrawlcontract.CrawlOutcomeNodeRejected
	OutcomeSitemap           = yagocrawlcontract.CrawlOutcomeSitemap
	OutcomeUnsupportedType   = yagocrawlcontract.CrawlOutcomeUnsupportedType
	OutcomeOutsideScope      = yagocrawlcontract.CrawlOutcomeOutsideScope
	OutcomeIndexingPolicy    = yagocrawlcontract.CrawlOutcomeIndexingPolicy
	OutcomeEmptyContent      = yagocrawlcontract.CrawlOutcomeEmptyContent
	OutcomeOther             = yagocrawlcontract.CrawlOutcomeOther
	maximumOutcomeReasons    = yagocrawlcontract.MaximumCrawlOutcomeReasons
)

func validatePageOutcome(state State, outcome PageOutcome) error {
	if outcome.Class == "" && outcome.Reason == "" {
		return nil
	}
	if state != StateFetched {
		return errors.New("page outcome requires fetched state")
	}
	switch outcome.Class {
	case OutcomeDocumentCandidate, OutcomeRemovalMutation:
		if outcome.Reason != "" {
			return errors.New("mutation outcome cannot include a reason")
		}
	case OutcomeNotIndexable:
		if !yagocrawlcontract.ValidNotIndexableOutcomeReason(outcome.Reason) {
			return errors.New("invalid not-indexable outcome reason")
		}
	default:
		return errors.New("invalid page outcome class")
	}
	return nil
}

func recordPageOutcome(depth *QueueDepth, page *Page, outcome PageOutcome) error {
	if err := validatePageOutcome(StateFetched, outcome); err != nil {
		return err
	}
	page.Outcome = outcome.Class
	page.OutcomeReason = outcome.Reason
	switch outcome.Class {
	case OutcomeDocumentCandidate:
		depth.DocumentCandidates++
	case OutcomeRemovalMutation:
		depth.RemovalMutations++
	case OutcomeNotIndexable:
		depth.NotIndexable++
		return incrementOutcomeCount(depth, outcome.Class, outcome.Reason)
	case "":
		return nil
	default:
		return errors.New("invalid page outcome class")
	}
	return nil
}

func recordNodeRejection(depth *QueueDepth, page *Page, rejectionRule string) error {
	reason := normalizedRejectionRule(rejectionRule)
	page.Outcome = OutcomeNodeRejected
	page.OutcomeReason = reason
	depth.NodeRejected++
	return incrementOutcomeCount(depth, OutcomeNodeRejected, reason)
}

func normalizedRejectionRule(rule string) string {
	if yagocrawlcontract.ValidCrawlRejectionRule(rule) {
		return rule
	}
	if rule == "" {
		return "unspecified"
	}
	return OutcomeOther
}

func incrementOutcomeCount(depth *QueueDepth, class, reason string) error {
	for index := range depth.OutcomeCounts {
		count := &depth.OutcomeCounts[index]
		if count.Class == class && count.Reason == reason {
			count.Pages++
			return nil
		}
	}
	if len(depth.OutcomeCounts) >= maximumOutcomeReasons {
		return errors.New("frontier outcome reason limit reached")
	}
	depth.OutcomeCounts = append(depth.OutcomeCounts, OutcomeCount{
		Class: class, Reason: reason, Pages: 1,
	})
	return nil
}
