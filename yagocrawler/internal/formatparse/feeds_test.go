package formatparse

import (
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
	if len(page.Links) != 3 || page.Links[0] != "https://a.example/e1" ||
		page.Links[1] != "https://a.example/e2" || page.Links[2] != "https://a.example/only-self" {
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

func TestFeedItemLimitClipsHostileFeeds(t *testing.T) {
	var rss strings.Builder
	rss.WriteString(`<rss version="2.0"><channel><title>big</title>`)
	for i := 0; i < feedItemLimit+50; i++ {
		rss.WriteString(`<item><title>x</title><link>https://a.example/l</link></item>`)
	}
	rss.WriteString(`</channel></rss>`)
	page, parsed := parseRSS("https://a.example/big.rss", []byte(rss.String()))
	if !parsed || len(page.Links) != feedItemLimit {
		t.Fatalf("rss clip = %v links=%d, want %d", parsed, len(page.Links), feedItemLimit)
	}

	var atom strings.Builder
	atom.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom"><title>big</title>`)
	for i := 0; i < feedItemLimit+50; i++ {
		atom.WriteString(`<entry><title>x</title><link href="https://a.example/l"/></entry>`)
	}
	atom.WriteString(`</feed>`)
	aPage, aParsed := parseAtom("https://a.example/big.atom", []byte(atom.String()))
	if !aParsed || len(aPage.Links) != feedItemLimit {
		t.Fatalf("atom clip = %v links=%d, want %d", aParsed, len(aPage.Links), feedItemLimit)
	}

	if got := atomEntryLink(atomEntry{}); got != "" {
		t.Fatalf("linkless atom entry = %q", got)
	}

	// A feed whose title is empty falls back to the first text line.
	titleless := `<rss version="2.0"><channel><item><title>Only item</title></item></channel></rss>`
	tPage, tParsed := parseRSS("https://a.example/t.rss", []byte(titleless))
	if !tParsed || tPage.Title != "Only item" {
		t.Fatalf("titleless feed = %v %+v", tParsed, tPage)
	}
}
