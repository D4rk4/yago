package runtally

import (
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (t *Tally) CommitPage(
	provenance []byte,
	rawURL string,
	page yagocrawlcontract.CrawlRunTally,
	reason string,
) {
	t.CommitPageWithStatus(provenance, rawURL, page, 0, reason)
}

func (t *Tally) CommitPageWithStatus(
	provenance []byte,
	rawURL string,
	page yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := string(provenance)
	t.sequence[key]++
	history := t.outcomes[key]
	history.Append(yagocrawlcontract.CrawlURLOutcome{
		Sequence:   t.sequence[key],
		URL:        boundedUTF8(rawURL, yagocrawlcontract.MaximumCrawlOutcomeURLBytes),
		Class:      crawlURLOutcomeClass(page),
		ObservedAt: t.now().UTC(),
		HTTPStatus: httpStatus,
		Reason: boundedUTF8(
			reason,
			yagocrawlcontract.MaximumCrawlOutcomeReasonBytes,
		),
	})
	t.outcomes[key] = history
}

func (t *Tally) RecentOutcomes(
	provenance []byte,
) yagocrawlcontract.CrawlURLOutcomeHistory {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.outcomes[string(provenance)]
}

func crawlURLOutcomeClass(
	page yagocrawlcontract.CrawlRunTally,
) yagocrawlcontract.CrawlURLOutcomeClass {
	switch {
	case page.RobotsDenied > 0:
		return yagocrawlcontract.CrawlURLOutcomeRobotsDenied
	case page.Failed > 0:
		return yagocrawlcontract.CrawlURLOutcomeFailed
	case page.Indexed > 0:
		return yagocrawlcontract.CrawlURLOutcomeIndexed
	case page.Fetched > 0:
		return yagocrawlcontract.CrawlURLOutcomeFetched
	case page.Duplicates > 0:
		return yagocrawlcontract.CrawlURLOutcomeDuplicate
	default:
		return yagocrawlcontract.CrawlURLOutcomeSkipped
	}
}

func boundedUTF8(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}

	return value
}
