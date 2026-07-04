package crawlseed_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlseed"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

type seedSource map[string]pagefetch.FetchedPage

func (s seedSource) Fetch(
	_ context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	page, ok := s[target.String()]
	if !ok {
		return pagefetch.FetchedPage{}, errors.New("missing fixture")
	}
	if page.URL == nil {
		page.URL = target
	}
	return page, nil
}

type countingSeedSource struct {
	pages seedSource
	calls int
}

func (s *countingSeedSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	s.calls++
	return s.pages.Fetch(ctx, target)
}

func TestExpanderPassesURLRequestsThrough(t *testing.T) {
	req := yagocrawlcontract.CrawlRequest{
		URL:           "https://example.org/",
		ProfileHandle: "profile",
	}
	got, err := crawlseed.NewExpander(nil, 10).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{req})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0].Mode != yagocrawlcontract.CrawlRequestModeURL {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderExpandsSitemapURLSet(t *testing.T) {
	lastmod := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	source := seedSource{
		"https://example.org/sitemap.xml": {
			Body: []byte(`<urlset>
<url><loc>/a</loc><lastmod>2026-07-01</lastmod></url>
<url><loc>ftp://example.org/ignored</loc></url>
</urlset>`),
		},
	}
	req := yagocrawlcontract.CrawlRequest{
		URL:           "https://example.org/sitemap.xml",
		Mode:          yagocrawlcontract.CrawlRequestModeSitemap,
		ProfileHandle: "profile",
	}

	got, err := crawlseed.NewExpander(source, 10).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{req})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 ||
		got[0].URL != "https://example.org/a" ||
		got[0].Mode != yagocrawlcontract.CrawlRequestModeURL ||
		got[0].ReferrerURL != req.URL ||
		got[0].LastModified != lastmod {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderExpandsSitemapIndex(t *testing.T) {
	source := seedSource{
		"https://example.org/sitemap-index.xml": {
			Body: []byte(`<sitemapindex>
<sitemap><loc>/a.xml</loc></sitemap>
<sitemap><loc>/b.xml</loc></sitemap>
</sitemapindex>`),
		},
		"https://example.org/a.xml": {
			Body: []byte(`<urlset><url><loc>/a</loc></url></urlset>`),
		},
		"https://example.org/b.xml": {
			Body: []byte(`<urlset><url><loc>/b</loc></url></urlset>`),
		},
	}
	req := yagocrawlcontract.CrawlRequest{
		URL:           "https://example.org/sitemap-index.xml",
		Mode:          yagocrawlcontract.CrawlRequestModeSitemap,
		ProfileHandle: "profile",
	}

	got, err := crawlseed.NewExpander(source, 1).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{req})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://example.org/a" {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderSkipsDuplicateSitemaps(t *testing.T) {
	source := seedSource{
		"https://example.org/index.xml": {
			Body: []byte(`<sitemapindex>
<sitemap><loc>/a.xml</loc></sitemap>
<sitemap><loc>/a.xml</loc></sitemap>
</sitemapindex>`),
		},
		"https://example.org/a.xml": {
			Body: []byte(`<urlset><url><loc>/a</loc></url></urlset>`),
		},
	}

	got, err := crawlseed.NewExpander(source, 100).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/index.xml",
			Mode: yagocrawlcontract.CrawlRequestModeSitemap,
		}})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://example.org/a" {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderCapsSitemapFiles(t *testing.T) {
	source := &countingSeedSource{pages: seedSource{}}
	var index strings.Builder
	index.WriteString("<sitemapindex>")
	for i := 0; i < 65; i++ {
		raw := fmt.Sprintf("https://example.org/%d.xml", i)
		index.WriteString("<sitemap><loc>")
		index.WriteString(raw)
		index.WriteString("</loc></sitemap>")
		source.pages[raw] = pagefetch.FetchedPage{Body: []byte("<urlset></urlset>")}
	}
	index.WriteString("</sitemapindex>")
	source.pages["https://example.org/index.xml"] = pagefetch.FetchedPage{
		Body: []byte(index.String()),
	}

	got, err := crawlseed.NewExpander(source, 100).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/index.xml",
			Mode: yagocrawlcontract.CrawlRequestModeSitemap,
		}})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("requests = %#v", got)
	}
	if source.calls != 64 {
		t.Fatalf("fetches = %d, want 64", source.calls)
	}
}

func TestExpanderExpandsSitelist(t *testing.T) {
	source := seedSource{
		"https://example.org/urls.txt": {
			Body: []byte("https://example.org/a\nhttps://example.org/b\n"),
		},
	}
	req := yagocrawlcontract.CrawlRequest{
		URL:           "https://example.org/urls.txt",
		Mode:          yagocrawlcontract.CrawlRequestModeSitelist,
		ProfileHandle: "profile",
		ReferrerURL:   "ignored",
	}

	got, err := crawlseed.NewExpander(source, 1).
		Expand(context.Background(), []yagocrawlcontract.CrawlRequest{req})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 ||
		got[0].URL != "https://example.org/a" ||
		got[0].ReferrerURL != "" {
		t.Fatalf("requests = %#v", got)
	}
}

func TestExpanderRejectsBadInputs(t *testing.T) {
	cases := [][]yagocrawlcontract.CrawlRequest{
		{{URL: "https://example.org/", Mode: "bad"}},
		{{URL: "://bad", Mode: yagocrawlcontract.CrawlRequestModeSitemap}},
		{{URL: "https://example.org/sitemap.xml", Mode: yagocrawlcontract.CrawlRequestModeSitemap}},
		{{URL: "https://example.org/list.txt", Mode: yagocrawlcontract.CrawlRequestModeSitelist}},
	}
	for _, requests := range cases {
		if _, err := crawlseed.NewExpander(nil, 10).
			Expand(context.Background(), requests); err == nil {
			t.Fatalf("requests %#v should fail", requests)
		}
	}
}

func TestExpanderRejectsFetchFailures(t *testing.T) {
	source := seedSource{}
	for _, mode := range []yagocrawlcontract.CrawlRequestMode{
		yagocrawlcontract.CrawlRequestModeSitemap,
		yagocrawlcontract.CrawlRequestModeSitelist,
	} {
		_, err := crawlseed.NewExpander(source, 10).Expand(
			context.Background(),
			[]yagocrawlcontract.CrawlRequest{{
				URL:  "https://example.org/missing",
				Mode: mode,
			}},
		)
		if err == nil {
			t.Fatalf("mode %q should fail", mode)
		}
	}
}

func TestExpanderRejectsInvalidSeedURLWithSource(t *testing.T) {
	_, err := crawlseed.NewExpander(seedSource{}, 10).Expand(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{{
			URL:  "://bad",
			Mode: yagocrawlcontract.CrawlRequestModeSitemap,
		}},
	)
	if err == nil {
		t.Fatal("invalid seed URL should fail")
	}
}

func TestExpanderSkipsInvalidExpandedURLs(t *testing.T) {
	source := seedSource{
		"https://example.org/sitemap.xml": {
			Body: []byte(`<urlset>
<url><loc>mailto:editor@example.org</loc></url>
<url><loc>http:/missing-host</loc></url>
</urlset>`),
		},
		"https://example.org/index.xml": {
			Body: []byte(`<sitemapindex>
<sitemap><loc>mailto:editor@example.org</loc></sitemap>
</sitemapindex>`),
		},
	}
	for _, raw := range []string{"https://example.org/sitemap.xml", "https://example.org/index.xml"} {
		got, err := crawlseed.NewExpander(source, 10).Expand(
			context.Background(),
			[]yagocrawlcontract.CrawlRequest{{
				URL:  raw,
				Mode: yagocrawlcontract.CrawlRequestModeSitemap,
			}},
		)
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("requests = %#v", got)
		}
	}
}

func TestExpanderRejectsInvalidSitemapXML(t *testing.T) {
	source := seedSource{
		"https://example.org/sitemap.xml": {Body: []byte("<bad>")},
	}
	_, err := crawlseed.NewExpander(source, 10).Expand(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{{
			URL:  "https://example.org/sitemap.xml",
			Mode: yagocrawlcontract.CrawlRequestModeSitemap,
		}},
	)
	if err == nil {
		t.Fatal("invalid sitemap should fail")
	}
}
