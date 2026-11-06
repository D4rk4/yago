package formatparse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>News feed</title><description>Daily news</description>
<item><title>First story</title><link>https://a.example/1</link><description>Alpha body</description></item>
<item><title>Second story</title><link>https://a.example/2</link><description>Beta body</description></item>
<item><title>No link story</title><description>Gamma body</description></item>
</channel></rss>`

const sampleAtom = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>Atom feed</title>
<entry><title>Entry one</title><link rel="alternate" href="https://a.example/e1"/><summary>Sum one</summary></entry>
<entry><title>Entry two</title><link rel="self" href="https://a.example/self"/><link href="https://a.example/e2"/><content>Body two</content></entry>
<entry><title>Entry three</title><link rel="self" href="https://a.example/only-self"/></entry>
</feed>`

func TestParseRSSFeed(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/feed.rss", "application/rss+xml", []byte(sampleRSS),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "News feed" {
		t.Fatalf("rss parse = %v %+v", parsed, page)
	}
	for _, want := range []string{"Daily news", "First story", "Beta body", "Gamma body"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("rss text missing %q", want)
		}
	}
	if len(page.FollowableLinks) != 2 || page.FollowableLinks[0] != "https://a.example/1" {
		t.Fatalf("rss links = %v", page.FollowableLinks)
	}
}

func TestParseAtomFeed(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/feed.atom", "application/atom+xml", []byte(sampleAtom),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Atom feed" {
		t.Fatalf("atom parse = %v %+v", parsed, page)
	}
	for _, want := range []string{"Entry one", "Sum one", "Body two"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("atom text missing %q", want)
		}
	}
	if len(page.Links) != 2 || page.Links[0] != "https://a.example/e1" ||
		page.Links[1] != "https://a.example/e2" {
		t.Fatalf("atom links = %v", page.Links)
	}
}

func TestParseGenericXMLAndEdges(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/data.xml", "application/xml",
		[]byte(`<?xml version="1.0"?><root><a>Alpha text</a><b attr="x">Beta</b><empty/></root>`),
		toggles,
	)
	if !parsed || !strings.Contains(page.Text, "Alpha text") ||
		!strings.Contains(page.Text, "Beta") {
		t.Fatalf("generic xml = %v %+v", parsed, page)
	}
	if page.Title != "Alpha text" {
		t.Fatalf("generic xml title = %q", page.Title)
	}

	if _, parsed := Parse(
		"https://a.example/empty.xml", "text/xml", []byte(`<root><a/></root>`), toggles,
	); parsed {
		t.Fatal("xml without character data must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/broken.xml", "text/xml", []byte(`<root`), toggles,
	); parsed {
		t.Fatal("malformed xml must stay unparsed")
	}
}

const rss1RDF = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/">
<channel rdf:about="https://a.example/"><title>RDF feed</title></channel>
<item rdf:about="https://a.example/r1"><title>RDF item</title><link>https://a.example/rdf/1</link></item>
</rdf:RDF>`

const atom03 = `<?xml version="1.0"?>
<feed xmlns="http://purl.org/atom/ns#">
<title>Atom 0.3 feed</title>
<entry><title>Old entry</title><link rel="alternate" href="https://a.example/old"/></entry>
</feed>`

const (
	emptyRSS  = `<?xml version="1.0"?><rss version="2.0"><channel><title>Empty feed</title></channel></rss>`
	emptyAtom = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Empty atom</title></feed>`
)

const baseInheritance = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom" xml:base="https://example.org/base/">
  <entry xml:base="articles/">
    <link rel="self" href="record.xml"/>
    <link rel="alternate" xml:base="../posts/" href="harbor"/>
  </entry>
</feed>`

const guidFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>Guid permalink</title><guid isPermaLink="true">https://a.example/g/1</guid></item>
<item><title>Guid not permalink</title><guid isPermaLink="false">https://a.example/g/2</guid></item>
<item><title>Guid no attribute</title><guid>https://a.example/g/3</guid></item>
<item><title>Guid body link wins</title><link>https://a.example/body</link><guid>https://a.example/g/4</guid></item>
</channel></rss>`

const badHrefFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<item><title>JavaScript href</title><link>javascript:alert(1)</link></item>
<item><title>Empty href</title><link></link></item>
<item><title>No href no guid</title><description>body only</description></item>
</channel></rss>`

type feedDiscoveryCase struct {
	name        string
	contentType string
	body        string
	wantParsed  bool
	wantTitle   string
	wantText    []string
	wantLinks   []string
}

func TestParseFeedDiscovery(t *testing.T) {
	cases := []feedDiscoveryCase{
		{
			"rss2", "application/rss+xml", sampleRSS, true, "News feed",
			[]string{"Daily news", "First story", "Beta body"},
			[]string{"https://a.example/1", "https://a.example/2"},
		},
		{
			"rss1-rdf", "application/xml", rss1RDF, true, "RDF feed",
			[]string{"RDF item"},
			[]string{"https://a.example/rdf/1"},
		},
		{
			"atom1", "application/atom+xml", sampleAtom, true, "Atom feed",
			[]string{"Entry one", "Sum one", "Body two"},
			[]string{"https://a.example/e1", "https://a.example/e2"},
		},
		{
			"atom03", "application/atom+xml", atom03, true, "Atom 0.3 feed",
			[]string{"Old entry"},
			[]string{"https://a.example/old"},
		},
		{
			"empty-rss", "application/rss+xml", emptyRSS, true, "Empty feed",
			nil, nil,
		},
		{
			"empty-atom", "application/atom+xml", emptyAtom, true, "Empty atom",
			nil, nil,
		},
		{
			"base-inheritance", "application/atom+xml", baseInheritance, true, "",
			nil,
			[]string{"https://example.org/base/posts/harbor"},
		},
		{
			"guid", "application/rss+xml", guidFeed, true, "",
			nil,
			[]string{"https://a.example/g/1", "https://a.example/g/3", "https://a.example/body"},
		},
		{
			"bad-href", "application/rss+xml", badHrefFeed, true, "",
			nil, nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFeedPage(t, tc)
		})
	}
}

func assertFeedPage(t *testing.T, tc feedDiscoveryCase) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/feed", tc.contentType, []byte(tc.body), toggles,
	)
	if parsed != tc.wantParsed {
		t.Fatalf("parsed = %v, want %v (page=%+v)", parsed, tc.wantParsed, page)
	}
	if !parsed {
		return
	}
	if tc.wantTitle != "" && page.Title != tc.wantTitle {
		t.Fatalf("title = %q, want %q", page.Title, tc.wantTitle)
	}
	for _, want := range tc.wantText {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("text missing %q in %q", want, page.Text)
		}
	}
	if len(page.FollowableLinks) != len(tc.wantLinks) {
		t.Fatalf(
			"links len = %d (%v), want %d (%v)",
			len(page.FollowableLinks),
			page.FollowableLinks,
			len(tc.wantLinks),
			tc.wantLinks,
		)
	}
	for i, want := range tc.wantLinks {
		if page.FollowableLinks[i] != want {
			t.Fatalf("links[%d] = %q, want %q", i, page.FollowableLinks[i], want)
		}
	}
}

func TestParseFeedPartialFailure(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	body := `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>First</title><link>https://a.example/1</link></item>
<item><title>Second</title><link>https://a.example/2</item></channel></rss>`
	page, parsed := Parse(
		"https://a.example/feed.rss", "application/rss+xml", []byte(body), toggles,
	)
	if parsed {
		t.Fatalf("truncated feed must report partial failure, got parsed=true")
	}
	if len(page.FollowableLinks) != 1 || page.FollowableLinks[0] != "https://a.example/1" {
		t.Fatalf("partial links = %v, want [https://a.example/1]", page.FollowableLinks)
	}
	if !strings.Contains(page.Text, "First") {
		t.Fatalf("partial text = %q, want to keep First", page.Text)
	}
}

func TestParseFeedTruncatedSecondItem(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	body := `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>First</title><link>https://a.example/1</link></item>
<item><title>Second</title><link>https://a.example/2</item>`
	page, parsed := Parse(
		"https://a.example/feed.rss", "application/rss+xml", []byte(body), toggles,
	)
	if parsed {
		t.Fatal("truncated feed must report partial failure")
	}
	if len(page.FollowableLinks) != 1 || page.FollowableLinks[0] != "https://a.example/1" {
		t.Fatalf("truncated links = %v, want only the first item once", page.FollowableLinks)
	}
}

func TestParseFeedOverLimit(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel>`)
	for i := 0; i < maxFeedItemCandidates+1; i++ {
		fmt.Fprintf(
			&b,
			`<item><title>Item %d</title><link>https://a.example/%d</link></item>`,
			i,
			i,
		)
	}
	b.WriteString(`</channel></rss>`)
	page, parsed := Parse(
		"https://a.example/feed.rss", "application/rss+xml", []byte(b.String()), toggles,
	)
	if !parsed {
		t.Fatalf("over-limit feed must still parse cleanly, got parsed=false")
	}
	if len(page.FollowableLinks) != maxFeedItemCandidates {
		t.Fatalf("over-limit links = %d, want %d", len(page.FollowableLinks), maxFeedItemCandidates)
	}
}

func TestParseFeedRecognizesRootNotEveryXML(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/data.xml", "application/xml",
		[]byte(`<?xml version="1.0"?><note><to>T</to><from>F</from></note>`), toggles,
	)
	if !parsed {
		t.Fatalf("generic xml must stay parsed through generic dispatch, got parsed=false")
	}
	if strings.Contains(page.Title, "note") || len(page.FollowableLinks) != 0 {
		t.Fatalf("non-feed xml must not be treated as a feed: %+v", page)
	}
}

func TestResolveBaseEdges(t *testing.T) {
	cases := []struct {
		parent, local, want string
	}{
		{"", "https://a.example/x", "https://a.example/x"},
		{"", "", ""},
		{"http://a.example/%zz", "local", "local"},
		{"https://a.example/base/", "http://a.example/%zz", "http://a.example/%zz"},
		{"https://a.example/base/", "../posts/harbor", "https://a.example/posts/harbor"},
	}
	for _, tc := range cases {
		if got := resolveBase(tc.parent, tc.local); got != tc.want {
			t.Fatalf("resolveBase(%q, %q) = %q, want %q", tc.parent, tc.local, got, tc.want)
		}
	}
}

func TestSelectItemLinkEdges(t *testing.T) {
	// rel that is neither absent, alternate, self, nor enclosure is skipped.
	next := &feedEntry{links: []feedLink{{rel: "next", href: "https://a.example/next"}}}
	if got := selectItemLink(next); got != "" {
		t.Fatalf("rel=next must be skipped, got %q", got)
	}

	// GUID without entry base returns the GUID value directly.
	noBase := &feedEntry{guid: feedGUID{value: "https://a.example/g", perma: "true", set: true}}
	if got := selectItemLink(noBase); got != "https://a.example/g" {
		t.Fatalf("guid without base = %q, want the guid value", got)
	}

	// GUID with entry base resolves against it.
	withBase := &feedEntry{
		base: "https://a.example/base/",
		guid: feedGUID{value: "https://a.example/g", perma: "true", set: true},
	}
	if got := selectItemLink(withBase); got != "https://a.example/g" {
		t.Fatalf("guid with base = %q, want resolved", got)
	}

	// isPermaLink="false" suppresses the GUID fallback.
	notPermalink := &feedEntry{
		guid: feedGUID{value: "https://a.example/g", perma: "false", set: true},
	}
	if got := selectItemLink(notPermalink); got != "" {
		t.Fatalf("isPermaLink=false must suppress guid, got %q", got)
	}
}

func BenchmarkParseFeed(b *testing.B) {
	body := []byte(sampleRSS)
	for i := 0; i < b.N; i++ {
		_, parsed := Parse(
			"https://a.example/feed.rss", "application/rss+xml", body,
			yagocrawlcontract.DefaultFormatToggles(),
		)
		if !parsed {
			b.Fatal("benchmark feed must parse")
		}
	}
}
