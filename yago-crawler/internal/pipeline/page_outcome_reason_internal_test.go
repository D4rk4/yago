package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type basicPageCompletionFrontier struct {
	completed crawljob.CrawlJob
	outcome   yagocrawlcontract.CrawlRunTally
}

type reasonPageCompletionFrontier struct {
	basicPageCompletionFrontier
	reason string
}

func (frontier *reasonPageCompletionFrontier) DoneWithReason(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	reason string,
) {
	frontier.completed = work
	frontier.outcome = outcome
	frontier.reason = reason
}

func (*basicPageCompletionFrontier) Take(context.Context) (crawljob.CrawlJob, bool) {
	return crawljob.CrawlJob{}, false
}

func (*basicPageCompletionFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (frontier *basicPageCompletionFrontier) Done(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
) {
	frontier.completed = work
	frontier.outcome = outcome
}

func (*basicPageCompletionFrontier) Abandon(crawljob.CrawlJob) {}

func (*basicPageCompletionFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool {
	return true
}

func TestPageCompletionReasonFallsBackToBasicFrontier(t *testing.T) {
	frontier := &basicPageCompletionFrontier{}
	job := crawljob.CrawlJob{URL: "https://example.com/"}
	outcome := yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1}
	completePage(frontier, job, outcome, 0, contentParserNoDocumentReason)
	if frontier.completed.URL != job.URL || frontier.outcome != outcome {
		t.Fatalf("completion = %+v %+v", frontier.completed, frontier.outcome)
	}
}

func TestPageCompletionEvidenceFallsBackToReasonFrontier(t *testing.T) {
	frontier := &reasonPageCompletionFrontier{}
	job := crawljob.CrawlJob{URL: "https://example.com/"}
	outcome := yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1}
	completePage(frontier, job, outcome, 200, contentParserNoDocumentReason)
	if frontier.completed.URL != job.URL || frontier.outcome != outcome ||
		frontier.reason != contentParserNoDocumentReason {
		t.Fatalf("completion = %+v %+v %q", frontier.completed, frontier.outcome, frontier.reason)
	}
}

func TestUnknownPageProcessingFailureUsesStableGenericReason(t *testing.T) {
	if reason := pageProcessingFailureReason(
		errors.New("secret provider detail"),
	); reason != pageFetchFailureReason {
		t.Fatalf("generic failure reason = %q", reason)
	}
}
