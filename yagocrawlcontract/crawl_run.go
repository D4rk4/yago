package yagocrawlcontract

import "slices"

const (
	CrawlOutcomeDocumentCandidate = "document_candidate"
	CrawlOutcomeRemovalMutation   = "removal_mutation"
	CrawlOutcomeNotIndexable      = "not_indexable"
	CrawlOutcomeNodeRejected      = "node_rejected"
	CrawlOutcomeSitemap           = "sitemap"
	CrawlOutcomeUnsupportedType   = "unsupported_content_type"
	CrawlOutcomeOutsideScope      = "outside_crawl_scope"
	CrawlOutcomeIndexingPolicy    = "indexing_policy"
	CrawlOutcomeEmptyContent      = "empty_content"
	CrawlOutcomeOther             = "other"
	MaximumCrawlOutcomeReasons    = 16
)

// CrawlRunState is the lifecycle stage a worker reports for a run. A run is
// Running while its frontier still has work, Finished when it drains, and
// Cancelled when the operator stops it.
type CrawlRunState string

const (
	CrawlRunRunning   CrawlRunState = "running"
	CrawlRunFinished  CrawlRunState = "finished"
	CrawlRunCancelled CrawlRunState = "cancelled"
)

// CrawlRunTally is the cumulative outcome of a run's pages. It is reported as an
// absolute snapshot, not a delta, so a lost report is corrected by the next one.
type CrawlRunTally struct {
	Fetched            uint64
	Indexed            uint64
	Failed             uint64
	RobotsDenied       uint64
	Duplicates         uint64
	Pending            uint64
	DocumentCandidates uint64
	RemovalMutations   uint64
	NotIndexable       uint64
	NodeRejected       uint64
	PendingIngest      uint64
	OutcomeCounts      []CrawlOutcomeCount
}

type CrawlOutcomeCount struct {
	Class  string
	Reason string
	Pages  uint64
}

func (tally CrawlRunTally) Equal(other CrawlRunTally) bool {
	return tally.Fetched == other.Fetched && tally.Indexed == other.Indexed &&
		tally.Failed == other.Failed && tally.RobotsDenied == other.RobotsDenied &&
		tally.Duplicates == other.Duplicates && tally.Pending == other.Pending &&
		tally.DocumentCandidates == other.DocumentCandidates &&
		tally.RemovalMutations == other.RemovalMutations &&
		tally.NotIndexable == other.NotIndexable && tally.NodeRejected == other.NodeRejected &&
		tally.PendingIngest == other.PendingIngest &&
		slices.Equal(tally.OutcomeCounts, other.OutcomeCounts)
}

func (tally CrawlRunTally) Valid() bool {
	if len(tally.OutcomeCounts) > MaximumCrawlOutcomeReasons ||
		tally.DocumentCandidates > tally.Fetched ||
		tally.RemovalMutations > tally.Fetched-tally.DocumentCandidates {
		return false
	}
	remaining := tally.Fetched
	for _, classified := range []uint64{
		tally.Indexed, tally.PendingIngest, tally.NotIndexable, tally.NodeRejected,
	} {
		if classified > remaining {
			return false
		}
		remaining -= classified
	}
	seen := make(map[string]struct{}, len(tally.OutcomeCounts))
	var notIndexablePages uint64
	var nodeRejectedPages uint64
	for _, outcome := range tally.OutcomeCounts {
		if outcome.Pages == 0 {
			return false
		}
		key := outcome.Class + "\x00" + outcome.Reason
		if _, found := seen[key]; found {
			return false
		}
		seen[key] = struct{}{}
		switch outcome.Class {
		case CrawlOutcomeNotIndexable:
			if !ValidNotIndexableOutcomeReason(outcome.Reason) ||
				outcome.Pages > tally.NotIndexable-notIndexablePages {
				return false
			}
			notIndexablePages += outcome.Pages
		case CrawlOutcomeNodeRejected:
			if !ValidCrawlRejectionRule(outcome.Reason) ||
				outcome.Pages > tally.NodeRejected-nodeRejectedPages {
				return false
			}
			nodeRejectedPages += outcome.Pages
		default:
			return false
		}
	}
	return notIndexablePages == tally.NotIndexable && nodeRejectedPages == tally.NodeRejected
}

func ValidNotIndexableOutcomeReason(reason string) bool {
	switch reason {
	case CrawlOutcomeSitemap, CrawlOutcomeUnsupportedType, CrawlOutcomeOutsideScope,
		CrawlOutcomeIndexingPolicy, CrawlOutcomeEmptyContent:
		return true
	default:
		return false
	}
}

func ValidCrawlRejectionRule(reason string) bool {
	switch reason {
	case "too-few-words", "too-many-words", "word-length", "symbol-ratio",
		"non-alphabetic", "no-function-words", "top-ngram", "repeated-ngram",
		"lorem-ipsum", "unspecified", CrawlOutcomeOther:
		return true
	default:
		return false
	}
}

type CrawlRunProgress struct {
	RunID             string
	WorkerID          string
	ProcessIdentifier string
	ProfileHandle     string
	ProfileName       string
	State             CrawlRunState
	Tally             CrawlRunTally
	RecentOutcomes    CrawlURLOutcomeHistory
	PagesPerMinute    uint32
	RateKnown         bool
	MaxPagesPerHost   int
	MaxPagesPerRun    int
	LimitsKnown       bool
}

func (progress CrawlRunProgress) Equal(other CrawlRunProgress) bool {
	return progress.RunID == other.RunID && progress.WorkerID == other.WorkerID &&
		progress.ProcessIdentifier == other.ProcessIdentifier &&
		progress.ProfileHandle == other.ProfileHandle && progress.ProfileName == other.ProfileName &&
		progress.State == other.State && progress.Tally.Equal(other.Tally) &&
		progress.RecentOutcomes == other.RecentOutcomes &&
		progress.PagesPerMinute == other.PagesPerMinute && progress.RateKnown == other.RateKnown &&
		progress.MaxPagesPerHost == other.MaxPagesPerHost &&
		progress.MaxPagesPerRun == other.MaxPagesPerRun && progress.LimitsKnown == other.LimitsKnown
}
