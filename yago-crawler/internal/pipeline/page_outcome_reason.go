package pipeline

import (
	"errors"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	crawlURLInvalidReason              = "crawl URL could not be parsed"
	robotsDeniedReason                 = "robots.txt denied the fetch"
	pageFetchFailureReason             = "page fetch failed"
	redirectNotAdmittedReason          = "redirect target was not admitted"
	unsupportedContentReason           = "content type is not enabled for this crawl"
	contentParserNoDocumentReason      = "content parser produced no indexable document"
	pageDirectivesNoindexReason        = "page directives disabled indexing"
	crawlProfileIndexDisabledReason    = "crawl profile disabled indexing"
	documentRejectedByNodeReason       = "node content-quality gate rejected the document"
	documentIndexFailureReason         = "document indexing failed"
	documentIngestFailureReason        = "document ingest delivery failed"
	documentRemovalIngestFailureReason = "document removal delivery failed"
)

var (
	errDocumentIndexFailure         = errors.New(documentIndexFailureReason)
	errDocumentIngestFailure        = errors.New(documentIngestFailureReason)
	errDocumentRemovalIngestFailure = errors.New(documentRemovalIngestFailureReason)
)

type pageOutcomeReasonFrontier interface {
	DoneWithReason(crawljob.CrawlJob, yagocrawlcontract.CrawlRunTally, string)
}

func completePage(
	frontier Frontier,
	job crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	if detailed, ok := frontier.(pageOutcomeEvidenceFrontier); ok {
		detailed.DoneWithPageOutcome(job, outcome, httpStatus, reason)

		return
	}
	if detailed, ok := frontier.(pageOutcomeReasonFrontier); ok {
		detailed.DoneWithReason(job, outcome, reason)

		return
	}
	frontier.Done(job, outcome)
}

func fetchOutcomeReason(err error) string {
	switch {
	case errors.Is(err, robots.ErrDisallowed):
		return robotsDeniedReason
	case errors.Is(err, pagefetch.ErrUnsupportedContentType):
		return unsupportedContentReason
	default:
		return pageFetchFailureReason
	}
}

func pageProcessingFailureReason(err error) string {
	switch {
	case errors.Is(err, errDocumentIndexFailure):
		return documentIndexFailureReason
	case errors.Is(err, errDocumentIngestFailure):
		return documentIngestFailureReason
	case errors.Is(err, errDocumentRemovalIngestFailure):
		return documentRemovalIngestFailureReason
	default:
		return pageFetchFailureReason
	}
}

// documentRejectedReason names the node's content-quality rule on the crawl
// run's per-URL outcome, so an operator seeing a fetched-but-unindexed page can
// tell why the node refused it.
func documentRejectedReason(rule string) string {
	if rule == "" {
		return documentRejectedByNodeReason
	}

	return documentRejectedByNodeReason + ": " + rule
}
