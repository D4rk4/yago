package formatparse

import (
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func atomDocument(attributes, entries string) string {
	return `<feed xmlns="http://www.w3.org/2005/Atom"` + attributes + `><title>Harbor news</title>` + entries + `</feed>`
}

func atomEntry(attributes, links string) string {
	return `<entry` + attributes + `><title>Story</title>` + links + `</entry>`
}

type articleLinkExpectation struct {
	name      string
	body      string
	wantLinks []string
}

func assertArticleLinks(t *testing.T, cases []articleLinkExpectation) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			page, parsed := parseXMLFeeds(feedDocumentURL, "", []byte(testCase.body))
			if !parsed {
				t.Fatal("feed was not parsed")
			}
			if !slices.Equal(page.FollowableLinks, testCase.wantLinks) {
				t.Fatalf("links = %q, want %q", page.FollowableLinks, testCase.wantLinks)
			}
		})
	}
}

func TestFeedLinkBaseResolution(t *testing.T) {
	assertArticleLinks(t, []articleLinkExpectation{
		{
			name: "chained bases",
			body: atomDocument(
				` xml:base="https://example.org/base/"`,
				atomEntry(
					` xml:base="articles/"`,
					`<link rel="self" href="record.xml"/><link rel="alternate" xml:base="../posts/" href="harbor"/>`,
				),
			),
			wantLinks: []string{"https://example.org/base/posts/harbor"},
		},
		{
			name:      "relative link without a base resolves against the document",
			body:      atomDocument(``, atomEntry(``, `<link href="/posts/harbor"/>`)),
			wantLinks: []string{"https://example.org/posts/harbor"},
		},
		{
			name: "root base",
			body: atomDocument(
				` xml:base="https://example.test/root/"`,
				atomEntry(``, `<link href="harbor"/>`),
			),
			wantLinks: []string{"https://example.test/root/harbor"},
		},
		{
			name: "channel base under the root base",
			body: `<rss version="2.0" xml:base="https://example.test/root/"><channel xml:base="news/">` +
				rssItem(
					`<link>harbor</link>`,
				) + `</channel></rss>`,
			wantLinks: []string{"https://example.test/root/news/harbor"},
		},
		{
			name: "RSS 1.0 root base",
			body: `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xml:base="https://example.test/root/">` +
				rssItem(
					`<link>harbor</link>`,
				) + `</rdf:RDF>`,
			wantLinks: []string{"https://example.test/root/harbor"},
		},
		{
			name: "entry base",
			body: atomDocument(
				``,
				atomEntry(` xml:base="https://example.test/entries/"`, `<link href="harbor"/>`),
			),
			wantLinks: []string{"https://example.test/entries/harbor"},
		},
		{
			name: "link base",
			body: atomDocument(
				``,
				atomEntry(``, `<link xml:base="https://example.test/links/" href="harbor"/>`),
			),
			wantLinks: []string{"https://example.test/links/harbor"},
		},
		{
			name: "unparsable entry base",
			body: atomDocument(
				``,
				atomEntry(` xml:base="https://example.test/%zz/"`, `<link href="harbor"/>`),
			),
			wantLinks: []string{"https://example.org/feeds/harbor"},
		},
		{
			name: "base attribute outside the XML namespace",
			body: atomDocument(
				` xmlns:other="https://example.org/other" other:base="https://example.test/root/"`,
				atomEntry(``, `<link href="harbor"/>`),
			),
			wantLinks: []string{"https://example.org/feeds/harbor"},
		},
	})
}

func TestFeedLinkSelection(t *testing.T) {
	overlongLink := "https://example.org/" + strings.Repeat(
		"a",
		yagocrawlcontract.MaximumCrawlURLBytes,
	)
	assertArticleLinks(t, []articleLinkExpectation{
		{
			name: "href attribute outside the empty namespace",
			body: atomDocument(
				` xmlns:other="https://example.org/other"`,
				atomEntry(``, `<link other:href="https://example.org/news/one"/>`),
			),
			wantLinks: nil,
		},
		{
			name: "rel attribute outside the empty namespace",
			body: atomDocument(
				` xmlns:other="https://example.org/other"`,
				atomEntry(``, `<link other:rel="self" href="https://example.org/news/one"/>`),
			),
			wantLinks: []string{"https://example.org/news/one"},
		},
		{
			name:      "script link",
			body:      atomDocument(``, atomEntry(``, `<link href="javascript:alert(1)"/>`)),
			wantLinks: nil,
		},
		{
			name:      "unparsable link",
			body:      atomDocument(``, atomEntry(``, `<link href="https://example.org/%zz"/>`)),
			wantLinks: nil,
		},
		{
			name: "missing and empty href",
			body: atomDocument(
				``,
				atomEntry(``, `<link rel="alternate"/>`)+atomEntry(``, `<link href=""/>`),
			),
			wantLinks: nil,
		},
		{
			name: "self, enclosure, and next relations",
			body: atomDocument(``, atomEntry(
				``,
				`<link rel="self" href="https://example.org/self"/><link rel="enclosure" href="https://example.org/audio.mp3"/><link rel="next" href="https://example.org/page/2"/>`,
			)),
			wantLinks: nil,
		},
		{
			name: "absent relation after a self link",
			body: atomDocument(``, atomEntry(
				``,
				`<link rel="self" href="https://example.org/self"/><link href="https://example.org/news/one"/>`,
			)),
			wantLinks: []string{"https://example.org/news/one"},
		},
		{
			name:      "link longer than the URL limit",
			body:      rssDocument(rssItem(`<link>` + overlongLink + `</link>`)),
			wantLinks: nil,
		},
	})
}

func TestFeedGUIDArticleLinks(t *testing.T) {
	overlongGUID := "https://example.org/" + strings.Repeat(
		"a",
		yagocrawlcontract.MaximumCrawlURLBytes,
	)
	assertArticleLinks(t, []articleLinkExpectation{
		{
			name:      "permalink GUID without a link",
			body:      rssDocument(rssItem(`<guid>https://example.org/news/guid</guid>`)),
			wantLinks: []string{"https://example.org/news/guid"},
		},
		{
			name: "GUID that is not a permalink",
			body: rssDocument(
				rssItem(`<guid isPermaLink="false">https://example.org/news/guid</guid>`),
			),
			wantLinks: nil,
		},
		{
			name:      "unparsable GUID",
			body:      rssDocument(rssItem(`<guid>https://example.org/%zz</guid>`)),
			wantLinks: nil,
		},
		{
			name:      "relative GUID",
			body:      rssDocument(rssItem(`<guid>/news/guid</guid>`)),
			wantLinks: nil,
		},
		{
			name: "link preferred over GUID",
			body: rssDocument(
				rssItem(
					`<guid>https://example.org/news/guid</guid><link>https://example.org/news/one</link>`,
				),
			),
			wantLinks: []string{"https://example.org/news/one"},
		},
		{
			name: "GUID replaces an unusable link",
			body: rssDocument(
				rssItem(
					`<link>javascript:alert(1)</link><guid>https://example.org/news/guid</guid>`,
				),
			),
			wantLinks: []string{"https://example.org/news/guid"},
		},
		{
			name:      "GUID longer than the URL limit",
			body:      rssDocument(rssItem(`<guid>` + overlongGUID + `</guid>`)),
			wantLinks: nil,
		},
	})
}
