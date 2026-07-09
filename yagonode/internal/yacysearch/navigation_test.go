package yacysearch

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func navFacets() []searchcore.FacetGroup {
	return []searchcore.FacetGroup{
		{Name: "host", Terms: []searchcore.FacetTerm{{Term: "example.org", Count: 5}}},
		// protocol has no refine operator, so its values stay plain counts.
		{Name: "protocol", Terms: []searchcore.FacetTerm{{Term: "https", Count: 9}}},
	}
}

func TestSearchRequestFromValuesCountAlias(t *testing.T) {
	t.Parallel()

	req, err := searchRequestFromValues(url.Values{"query": {"golang"}, "count": {"5"}})
	if err != nil {
		t.Fatalf("count alias: %v", err)
	}
	if req.Limit != 5 {
		t.Fatalf("limit = %d, want 5 from the count alias", req.Limit)
	}
	// maximumRecords is the primary name and wins when both are present.
	both, err := searchRequestFromValues(url.Values{
		"query": {"golang"}, "count": {"5"}, "maximumRecords": {"8"},
	})
	if err != nil {
		t.Fatalf("both records params: %v", err)
	}
	if both.Limit != 8 {
		t.Fatalf("limit = %d, want 8 (maximumRecords over count)", both.Limit)
	}
}

func TestSearchRequestFromValuesAuthor(t *testing.T) {
	t.Parallel()

	// The author: operator used to be parsed then dropped; it must reach the request.
	fromOperator, err := searchRequestFromValues(
		url.Values{"query": {"relativity author:einstein"}},
	)
	if err != nil {
		t.Fatalf("author operator: %v", err)
	}
	if fromOperator.Author != "einstein" {
		t.Fatalf("author = %q, want einstein from the operator", fromOperator.Author)
	}
	// An explicit author param wins over the inline operator.
	fromParam, err := searchRequestFromValues(url.Values{
		"query": {"relativity author:einstein"}, "author": {"bohr"},
	})
	if err != nil {
		t.Fatalf("author param: %v", err)
	}
	if fromParam.Author != "bohr" {
		t.Fatalf("author = %q, want bohr from the explicit param", fromParam.Author)
	}
}

func TestSearchRequestFromValuesNavEnablesFacets(t *testing.T) {
	t.Parallel()

	withNav, err := searchRequestFromValues(url.Values{"query": {"golang"}, "nav": {"hosts"}})
	if err != nil {
		t.Fatalf("with nav: %v", err)
	}
	if !withNav.WithFacets {
		t.Fatal("a nav request must ask the index for facet counts")
	}
	without, err := searchRequestFromValues(url.Values{"query": {"golang"}})
	if err != nil {
		t.Fatalf("without nav: %v", err)
	}
	if without.WithFacets {
		t.Fatal("no nav must keep the facet scan off")
	}
}

func TestBuildNavigation(t *testing.T) {
	t.Parallel()

	groups := buildNavigation("golang", navFacets())
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	// host maps to the "hosts" navigator id and carries a refine modifier + URL.
	host := groups[0]
	if host.name != "hosts" || host.displayName != "Provider" {
		t.Fatalf("host group = %q/%q", host.name, host.displayName)
	}
	element := host.elements[0]
	if element.modifier != "site:example.org" {
		t.Fatalf("host modifier = %q", element.modifier)
	}
	if !strings.Contains(element.url, "site%3Aexample.org") {
		t.Fatalf("host url = %q", element.url)
	}
	// protocol has no operator, so it stays a plain count with no modifier.
	if protocol := groups[1]; protocol.name != "protocols" || protocol.elements[0].modifier != "" {
		t.Fatalf("protocol group = %q modifier=%q", protocol.name, protocol.elements[0].modifier)
	}
}

func TestFacetNavNameFallback(t *testing.T) {
	t.Parallel()

	if got := facetNavName("host"); got != "hosts" {
		t.Fatalf("host -> %q, want hosts", got)
	}
	// An unmapped dimension renders under its own name rather than vanishing.
	if got := facetNavName("mystery"); got != "mystery" {
		t.Fatalf("unmapped -> %q, want mystery", got)
	}
}

func TestResponseJSONNavigation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.json?query=golang", nil,
	)
	resp := searchcore.Response{
		Request: searchcore.Request{Query: "golang", Limit: 10},
		Facets:  navFacets(),
	}
	nav := responseJSON(req, resp).Channels[0].Navigation
	if len(nav) != 2 {
		t.Fatalf("navigation count = %d, want 2", len(nav))
	}
	host := nav[0]
	if host.FacetName != "hosts" || host.DisplayName != "Provider" || host.Type != "String" {
		t.Fatalf("host nav = %+v", host)
	}
	if host.Elements[0]["name"] != "example.org" || host.Elements[0]["count"] != "5" ||
		host.Elements[0]["modifier"] != "site:example.org" {
		t.Fatalf("host element = %+v", host.Elements[0])
	}
	// A dimension without an operator emits no modifier/url keys.
	if _, ok := nav[1].Elements[0]["modifier"]; ok {
		t.Fatalf("protocol element carries a modifier: %+v", nav[1].Elements[0])
	}
}

func TestResponseRSSNavigation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.rss?query=golang", nil,
	)
	resp := searchcore.Response{
		Request: searchcore.Request{Query: "golang", Limit: 10},
		Facets:  navFacets(),
	}
	facets := responseRSS(req, resp).Channel.Navigation.Facets
	if len(facets) != 2 {
		t.Fatalf("facet count = %d, want 2", len(facets))
	}
	if facets[0].Name != "hosts" || facets[0].Elements[0].Modifier != "site:example.org" ||
		facets[0].Elements[0].Count != 5 {
		t.Fatalf("host facet = %+v", facets[0])
	}
	// The wire element names must marshal into YaCy's yacy:navigation shape.
	encoded, err := xml.Marshal(responseRSS(req, resp))
	if err != nil {
		t.Fatalf("marshal rss: %v", err)
	}
	for _, want := range []string{
		`<yacy:navigation>`,
		`<facet name="hosts" displayname="Provider" type="String">`,
		`<modifier>site:example.org</modifier>`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("rss navigation missing %q in %s", want, encoded)
		}
	}
}

func TestResponseRSSItemRendersDublinCoreMetadata(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.rss?query=golang", nil,
	)
	resp := searchcore.Response{
		TotalResults: 1,
		Request:      searchcore.Request{Query: "golang", Limit: 10},
		Results: []searchcore.Result{{
			Title:     "Doc",
			URL:       "https://example.org/doc",
			Author:    "Ada Lovelace",
			Keywords:  "go, search",
			Publisher: "Example Press",
		}},
	}

	feed := responseRSS(req, resp)
	if len(feed.Channel.Items) != 1 {
		t.Fatalf("items = %+v", feed.Channel.Items)
	}
	item := feed.Channel.Items[0]
	if item.Creator != "Ada Lovelace" || item.Publisher != "Example Press" ||
		item.Subject != "go, search" {
		t.Fatalf("item = %+v", item)
	}
	encoded, err := xml.Marshal(feed)
	if err != nil {
		t.Fatalf("marshal rss: %v", err)
	}
	for _, want := range []string{
		`<dc:creator>Ada Lovelace</dc:creator>`,
		`<dc:publisher>Example Press</dc:publisher>`,
		`<dc:subject>go, search</dc:subject>`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("rss item missing %q in %s", want, encoded)
		}
	}
}
