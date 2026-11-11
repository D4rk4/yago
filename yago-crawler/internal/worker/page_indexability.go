package worker

import (
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func classifyPageIndexability(
	mediaType string,
	sitemap bool,
	inCrawlScope bool,
	indexingAllowed bool,
	hasContent bool,
) frontier.PageOutcome {
	if sitemap {
		return frontier.PageOutcome{Class: frontier.OutcomeNotIndexable, Reason: frontier.OutcomeSitemap}
	}
	if !strings.EqualFold(mediaType, "text/html") &&
		!strings.EqualFold(mediaType, "application/xhtml+xml") {
		return frontier.PageOutcome{Class: frontier.OutcomeNotIndexable, Reason: frontier.OutcomeUnsupportedType}
	}
	if !inCrawlScope {
		return frontier.PageOutcome{Class: frontier.OutcomeNotIndexable, Reason: frontier.OutcomeOutsideScope}
	}
	if !indexingAllowed {
		return frontier.PageOutcome{Class: frontier.OutcomeNotIndexable, Reason: frontier.OutcomeIndexingPolicy}
	}
	if !hasContent {
		return frontier.PageOutcome{Class: frontier.OutcomeNotIndexable, Reason: frontier.OutcomeEmptyContent}
	}
	return frontier.PageOutcome{Class: frontier.OutcomeDocumentCandidate}
}
