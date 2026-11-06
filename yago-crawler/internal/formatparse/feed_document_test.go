package formatparse

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const feedDocumentURL = "https://example.org/feeds/news.xml"

func rssDocument(channel string) string {
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Harbor news</title>` + channel + `</channel></rss>`
}

func rssItem(fields string) string {
	return `<item>` + fields + `</item>`
}

func repeatedRSSItems(count int, fields string) string {
	return strings.Repeat(rssItem(fields), count)
}

type feedPageExpectation struct {
	name       string
	body       string
	wantParsed bool
	wantTitle  string
	wantText   string
	wantLinks  []string
}

func assertFeedPages(t *testing.T, cases []feedPageExpectation) {
	t.Helper()
	for _, expectation := range cases {
		t.Run(expectation.name, func(t *testing.T) {
			assertFeedPage(t, expectation)
		})
	}
}

func assertFeedPage(t *testing.T, expectation feedPageExpectation) {
	t.Helper()
	page, parsed := parseXMLFeeds(feedDocumentURL, "", []byte(expectation.body))
	if parsed != expectation.wantParsed {
		t.Fatalf("parsed = %v, want %v", parsed, expectation.wantParsed)
	}
	if page.Title != expectation.wantTitle || page.Text != expectation.wantText {
		t.Fatalf("page title %q text %q, want title %q text %q",
			page.Title, page.Text, expectation.wantTitle, expectation.wantText)
	}
	if !slices.Equal(page.FollowableLinks, expectation.wantLinks) {
		t.Fatalf("links = %q, want %q", page.FollowableLinks, expectation.wantLinks)
	}
}

func TestFeedRecognitionRefusesOtherRoots(t *testing.T) {
	cases := []feedPageExpectation{
		{
			name:       "feed element under another root",
			body:       `<note><feed>x</feed><body>important text</body></note>`,
			wantParsed: true,
			wantTitle:  "x",
			wantText:   "x\nimportant text",
		},
		{
			name:       "rss root in a namespace",
			body:       `<rss xmlns="https://example.org/not-rss"><channel><item><title>Story</title><link>https://example.org/news/one</link></item></channel></rss>`,
			wantParsed: true,
			wantTitle:  "Story",
			wantText:   "Story\nhttps://example.org/news/one",
		},
		{
			name:       "RDF root outside the RDF namespace",
			body:       `<RDF xmlns="https://example.org/not-rdf"><item><title>Story</title><link>https://example.org/news/one</link></item></RDF>`,
			wantParsed: true,
			wantTitle:  "Story",
			wantText:   "Story\nhttps://example.org/news/one",
		},
		{
			name:       "feed root outside the Atom namespaces",
			body:       `<feed xmlns="https://example.org/not-atom"><entry><title>Entry</title><link href="https://example.org/news/one"/></entry></feed>`,
			wantParsed: true,
			wantTitle:  "Entry",
			wantText:   "Entry",
		},
	}
	assertFeedPages(t, cases)
}

func TestFeedRecognitionAcceptsFeedRoots(t *testing.T) {
	cases := []feedPageExpectation{
		{
			name: "RSS 2.0",
			body: rssDocument(
				`<description>Port</description>` + rssItem(
					`<title>Story</title><link>https://example.org/news/one</link>`,
				),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Port\nStory",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name: "RSS 1.0",
			body: `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">` +
				`<channel><title>Harbor news</title><description>Port</description></channel>` +
				`<item><title>Story</title><link>https://example.org/news/one</link></item></rdf:RDF>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Port\nStory",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name: "Atom 1.0",
			body: `<feed xmlns="http://www.w3.org/2005/Atom"><title>Harbor news</title>` +
				`<entry><title>Story</title><summary>Docks</summary><link href="https://example.org/news/one"/></entry></feed>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story\nDocks",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name: "Atom 0.3",
			body: `<feed xmlns="http://purl.org/atom/ns#"><title>Harbor news</title>` +
				`<entry><title>Story</title><link rel="alternate" href="https://example.org/news/one"/></entry></feed>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name:       "valid empty RSS",
			body:       rssDocument(``),
			wantParsed: true,
			wantTitle:  "Harbor news",
		},
		{
			name:       "valid empty Atom",
			body:       `<feed xmlns="http://www.w3.org/2005/Atom"><title>Harbor news</title></feed>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
		},
	}
	assertFeedPages(t, cases)
}

func TestFeedEntriesAreDirectEntryChildren(t *testing.T) {
	cases := []feedPageExpectation{
		{
			name: "RSS item outside the channel",
			body: `<rss version="2.0"><item><title>Stray</title><link>https://example.org/news/stray</link></item>` +
				`<channel><title>Harbor news</title>` + rssItem(
				`<title>Story</title><link>https://example.org/news/one</link>`,
			) + `</channel></rss>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name: "RSS 1.0 item inside the channel",
			body: `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">` +
				`<channel><title>Harbor news</title><item><title>Stray</title><link>https://example.org/news/stray</link></item></channel>` +
				`<item><title>Story</title><link>https://example.org/news/one</link></item></rdf:RDF>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
			wantLinks:  []string{"https://example.org/news/one"},
		},
		{
			name: "item nested inside an item",
			body: rssDocument(rssItem(`<title>Outer</title>` +
				rssItem(`<title>Inner</title><link>https://example.org/news/inner</link>`) +
				`<link>https://example.org/news/outer</link>`)),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Outer",
			wantLinks:  []string{"https://example.org/news/outer"},
		},
		{
			name: "Atom channel child",
			body: `<feed xmlns="http://www.w3.org/2005/Atom"><title>Harbor news</title>` +
				`<channel><title>Stray</title><entry><title>Stray</title><link href="https://example.org/news/stray"/></entry></channel>` +
				`<entry><title>Story</title><link href="https://example.org/news/one"/></entry></feed>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
			wantLinks:  []string{"https://example.org/news/one"},
		},
	}
	assertFeedPages(t, cases)
}

func TestBrokenFeedKeepsOnlyClosedEntries(t *testing.T) {
	cases := []feedPageExpectation{
		{
			name: "body ends inside the second item",
			body: `<rss version="2.0"><channel><title>Harbor news</title>` +
				rssItem(`<title>Story</title><link>https://example.org/news/one</link>`) +
				`<item><title>Unfinished</title><link>https://example.org/news/two</link>`,
			wantTitle: "Harbor news",
			wantText:  "Story",
			wantLinks: []string{"https://example.org/news/one"},
		},
		{
			name: "body ends inside the second item's description",
			body: `<rss version="2.0"><channel><title>Harbor news</title>` +
				rssItem(`<title>Story</title><link>https://example.org/news/one</link>`) +
				`<item><link>https://example.org/news/two</link><description>Unfinis`,
			wantTitle: "Harbor news",
			wantText:  "Story",
			wantLinks: []string{"https://example.org/news/one"},
		},
		{
			name: "error before the first closed item",
			body: rssDocument(
				`<item><title>Story</title><link>https://example.org/news/one</link></channel>`,
			),
			wantTitle: "Harbor news",
		},
		{
			name: "items opened inside each other and never closed",
			body: `<rss version="2.0"><channel><title>Harbor news</title>` +
				strings.Repeat(`<item><link>https://example.org/news/open</link>`, 500),
			wantTitle: "Harbor news",
		},
	}
	assertFeedPages(t, cases)
}

func TestFeedExaminesAtMostTheEntryCandidateLimit(t *testing.T) {
	linkedItem := `<link>https://example.org/news/last</link>`
	cases := []feedPageExpectation{
		{
			name: "linked item is the last candidate",
			body: rssDocument(
				repeatedRSSItems(feedEntryCandidateLimit-1, ``) + rssItem(linkedItem),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantLinks:  []string{"https://example.org/news/last"},
		},
		{
			name: "linked item follows the last candidate",
			body: rssDocument(
				repeatedRSSItems(feedEntryCandidateLimit, ``) + rssItem(linkedItem),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
		},
	}
	assertFeedPages(t, cases)
}

func TestFeedTextReadsOnlyDirectCharacterData(t *testing.T) {
	cases := []feedPageExpectation{
		{
			name: "character data sections wrapped in whitespace",
			body: rssDocument(
				rssItem(
					"<title>\n<![CDATA[Story]]>\n</title><description>\n<![CDATA[Body text]]>\n</description>",
				),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story\nBody text",
		},
		{
			name: "channel image title",
			body: rssDocument(
				`<image><title>Logo</title><url>https://example.org/logo.png</url></image>` + rssItem(
					`<title>Story</title>`,
				),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
		},
		{
			name: "markup and comments inside fields",
			body: rssDocument(
				rssItem(
					`<title>A<b>bold</b>B</title><description>Al<!-- note -->pha</description>`,
				),
			),
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "AB\nAlpha",
		},
		{
			name: "repeated fields keep the last",
			body: `<rss version="2.0"><channel><title>One</title><title>Two</title>` +
				rssItem(
					`<title>a</title><title>b</title><description>c</description><description></description>`,
				) + `</channel></rss>`,
			wantParsed: true,
			wantTitle:  "Two",
			wantText:   "b",
		},
		{
			name: "RSS title outside the channel",
			body: `<rss version="2.0"><channel><title>Harbor news</title>` +
				rssItem(`<title>Story</title>`) + `</channel><title>Stray</title></rss>`,
			wantParsed: true,
			wantTitle:  "Harbor news",
			wantText:   "Story",
		},
		{
			name:       "escaped Atom content",
			body:       `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>E</title><content type="html"><![CDATA[<p>x</p>]]></content></entry></feed>`,
			wantParsed: true,
			wantTitle:  "E",
			wantText:   "E\n<p>x</p>",
		},
	}
	assertFeedPages(t, cases)
}

func TestFeedTextFieldStopsAtTheDocumentTextLimit(t *testing.T) {
	field := strings.Repeat(
		"a",
		yagocrawlcontract.MaximumDocumentTextBytes-1,
	) + "é" + strings.Repeat(
		"b",
		64,
	)
	page, parsed := parseXMLFeeds(
		feedDocumentURL,
		"",
		[]byte(rssDocument(rssItem(`<description>`+field+`</description>`))),
	)
	if !parsed {
		t.Fatal("feed with a long description was not parsed")
	}
	if len(page.Text) != yagocrawlcontract.MaximumDocumentTextBytes-1 ||
		!utf8.ValidString(page.Text) {
		t.Fatalf("description length %d valid %v", len(page.Text), utf8.ValidString(page.Text))
	}
}

func BenchmarkParseFeed(b *testing.B) {
	ordinaryItem := rssItem(
		`<title>Story</title><link>https://example.org/news/one</link><description>Ships arrived at the harbor.</description>`,
	)
	fixtures := []struct {
		name string
		body []byte
	}{
		{name: "empty", body: []byte(rssDocument(``))},
		{name: "ordinary", body: []byte(sampleRSS)},
		{
			name: "entry candidate limit",
			body: []byte(rssDocument(strings.Repeat(ordinaryItem, feedEntryCandidateLimit))),
		},
		{name: "oversized", body: []byte(rssDocument(strings.Repeat(ordinaryItem, 30_000)))},
	}
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			b.SetBytes(int64(len(fixture.body)))
			b.ReportAllocs()
			for b.Loop() {
				parseXMLFeeds(feedDocumentURL, "", fixture.body)
			}
		})
	}
}
