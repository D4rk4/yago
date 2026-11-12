package worker

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func TestClassifyPageIndexability(t *testing.T) {
	cases := []struct {
		name    string
		media   string
		sitemap bool
		inScope bool
		allowed bool
		content bool
		class   string
		reason  string
	}{
		{name: "document content", media: "text/html", inScope: true, allowed: true, content: true, class: frontier.OutcomeDocumentCandidate},
		{name: "sitemap", media: "application/xml", sitemap: true, class: frontier.OutcomeNotIndexable, reason: frontier.OutcomeSitemap},
		{name: "unsupported media type", media: "application/pdf", inScope: true, class: frontier.OutcomeNotIndexable, reason: frontier.OutcomeUnsupportedType},
		{name: "outside crawl scope", media: "text/html", inScope: false, allowed: true, content: true, class: frontier.OutcomeNotIndexable, reason: frontier.OutcomeOutsideScope},
		{name: "indexing policy", media: "text/html", inScope: true, allowed: false, content: true, class: frontier.OutcomeNotIndexable, reason: frontier.OutcomeIndexingPolicy},
		{name: "empty extraction", media: "application/xhtml+xml", inScope: true, allowed: true, class: frontier.OutcomeNotIndexable, reason: frontier.OutcomeEmptyContent},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := classifyPageIndexability(
				testCase.media,
				testCase.sitemap,
				testCase.inScope,
				testCase.allowed,
				testCase.content,
			)
			if got.Class != testCase.class || got.Reason != testCase.reason {
				t.Fatalf("page indexability = %+v, want class %q reason %q", got, testCase.class, testCase.reason)
			}
		})
	}
}
