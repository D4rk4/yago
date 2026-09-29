package formatparse

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const sitemapXMLNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

func TestParseSitemapXMLAsNonindexablePage(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "url set",
			body: "\ufeff<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- sitemap -->\n" +
				`<urlset xmlns="` + sitemapXMLNamespace + `"><url><loc>https://alpha.example.test/one</loc></url><url><loc>https://alpha.example.test/two</loc></url></urlset>`,
		},
		{
			name: "sitemap index",
			body: `<sitemapindex xmlns="` + sitemapXMLNamespace + `"><sitemap><loc>https://alpha.example.test/map-a.xml</loc></sitemap></sitemapindex>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, parsed := Parse(
				"https://alpha.example.test/sitemap.xml",
				"application/xml",
				[]byte(tc.body),
				yagocrawlcontract.DefaultFormatToggles(),
			)
			if !parsed {
				t.Fatal("valid sitemap was not parsed")
			}
			if !page.Sitemap || page.Text != "" || page.Title != "" {
				t.Fatalf("sitemap text/title = %q/%q, want empty", page.Text, page.Title)
			}
			if len(page.FollowableLinks) != 0 {
				t.Fatalf(
					"sitemap parser supplied links before pipeline expansion: %v",
					page.FollowableLinks,
				)
			}
		})
	}
}

func TestParseRejectsSitemapWithTrailingDocumentContent(t *testing.T) {
	cases := []struct {
		name    string
		trailer string
	}{
		{name: "second root", trailer: `<extra>text</extra>`},
		{name: "trailing text", trailer: `extra text`},
		{name: "xml declaration", trailer: `<?xml version="1.0"?>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `<urlset xmlns="` + sitemapXMLNamespace + `"><url><loc>https://alpha.example.test/page</loc></url></urlset>` + tc.trailer
			page, parsed := Parse(
				"https://alpha.example.test/sitemap.xml",
				"application/xml",
				[]byte(body),
				yagocrawlcontract.DefaultFormatToggles(),
			)
			if parsed || !page.Sitemap || page.Text != "" || len(page.FollowableLinks) != 0 {
				t.Fatalf(
					"trailing sitemap content parse = %v, text=%q, links=%v",
					parsed,
					page.Text,
					page.FollowableLinks,
				)
			}
		})
	}
}

func TestParseSitemapXMLAcceptsLegalTrailerControls(t *testing.T) {
	body := `<urlset xmlns="` + sitemapXMLNamespace + `"><url><loc>https://alpha.example.test/page</loc></url></urlset>` +
		"\n\t<!-- done -->\n<?process complete?>\n\t"
	page, parsed := Parse(
		"https://alpha.example.test/sitemap.xml",
		"application/xml",
		[]byte(body),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || !page.Sitemap || page.Text != "" {
		t.Fatalf("sitemap with legal trailer controls = %v, %+v", parsed, page)
	}
}

func TestParseMalformedSitemapTrailerAsParseFailure(t *testing.T) {
	body := `<urlset xmlns="` + sitemapXMLNamespace + `"><url><loc>https://alpha.example.test/page</loc></url></urlset></extra>`
	page, parsed := Parse(
		"https://alpha.example.test/sitemap.xml",
		"application/xml",
		[]byte(body),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if parsed || !page.Sitemap || page.Text != "" || len(page.FollowableLinks) != 0 {
		t.Fatalf(
			"malformed sitemap trailer = %v, text=%q links=%v",
			parsed,
			page.Text,
			page.FollowableLinks,
		)
	}
}

func TestParseNonSitemapXMLAsGenericXML(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "wrong namespace",
			body: `<urlset xmlns="urn:other"><url><loc>https://alpha.example.test/page</loc></url></urlset>`,
		},
		{
			name: "wrong root",
			body: `<catalog xmlns="` + sitemapXMLNamespace + `"><record>Alpha data</record></catalog>`,
		},
		{
			name: "non-whitespace prefix",
			body: `Alpha prefix<urlset xmlns="` + sitemapXMLNamespace + `"><url><loc>https://alpha.example.test/page</loc></url></urlset>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, parsed := Parse(
				"https://alpha.example.test/data.xml",
				"application/xml",
				[]byte(tc.body),
				yagocrawlcontract.DefaultFormatToggles(),
			)
			if !parsed || page.Sitemap || page.Text == "" || len(page.FollowableLinks) != 0 {
				t.Fatalf("non-sitemap XML parse = %v, %+v", parsed, page)
			}
		})
	}
}

func TestParseMalformedSitemapXMLWithoutGenericText(t *testing.T) {
	page, parsed := Parse(
		"https://alpha.example.test/sitemap.xml",
		"application/xml",
		[]byte(
			`<urlset xmlns="`+sitemapXMLNamespace+`"><url><loc>https://alpha.example.test/page</loc><lastmod>2026-01-01`,
		),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if parsed || !page.Sitemap || page.Text != "" || len(page.FollowableLinks) != 0 {
		t.Fatalf(
			"malformed sitemap parse = %v, text=%q links=%v",
			parsed,
			page.Text,
			page.FollowableLinks,
		)
	}
}
