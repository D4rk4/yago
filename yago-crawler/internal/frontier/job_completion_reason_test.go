package frontier_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pageReasonTally struct {
	url     string
	outcome yagocrawlcontract.CrawlRunTally
	reason  string
}

func TestDoneWithPageOutcomeRecordsHTTPStatus(t *testing.T) {
	tally := runtally.New()
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithRunTally(tally))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	crawlFrontier.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		[]byte("run-with-status"),
		profile,
		nil,
	)
	job := receiveJob(t, crawlFrontier)
	crawlFrontier.DoneWithPageOutcome(
		job,
		yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
		200,
		"",
	)
	stored := tally.RecentOutcomes(job.Provenance).Chronological()[0]
	if stored.HTTPStatus != 200 {
		t.Fatalf("HTTP status = %d, want 200", stored.HTTPStatus)
	}
}

func (*pageReasonTally) Commit([]byte, yagocrawlcontract.CrawlRunTally) {}

func (*pageReasonTally) Snapshot([]byte) yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{}
}

func (*pageReasonTally) Restore([]byte, yagocrawlcontract.CrawlRunTally) {}

func (tally *pageReasonTally) CommitPage(
	_ []byte,
	url string,
	outcome yagocrawlcontract.CrawlRunTally,
	reason string,
) {
	tally.url = url
	tally.outcome = outcome
	tally.reason = reason
}

func TestDoneWithReasonRecordsBoundedPageEvidence(t *testing.T) {
	tally := &pageReasonTally{}
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithRunTally(tally))
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	crawlFrontier.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		[]byte("run-with-reason"),
		profile,
		nil,
	)
	job := receiveJob(t, crawlFrontier)
	outcome := yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1}
	crawlFrontier.DoneWithReason(job, outcome, "parser produced no document")
	if tally.url != job.URL || tally.outcome != outcome ||
		tally.reason != "parser produced no document" {
		t.Fatalf("recorded outcome = %q %+v %q", tally.url, tally.outcome, tally.reason)
	}
}
