package pipeline

import (
	"github.com/D4rk4/yago/yago-crawler/internal/formatparse"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yago-crawler/internal/sitemap"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func WithSitemapURLLimit(limit int) Option {
	return func(p *Pipeline) { p.sitemapURLLimit = limit }
}

func sitemapFollowableLinks(body []byte, limit int) []string {
	document, _ := sitemap.ParseXML(body, limit)
	entries := document.URLs
	if len(document.Sitemaps) > 0 {
		entries = document.Sitemaps
	}
	links := make([]string, 0, len(entries))
	for _, entry := range entries {
		links = append(links, entry.URL)
	}

	return links
}

func parseCrawlContent(
	rawURL string,
	contentType string,
	body []byte,
	formats yagocrawlcontract.FormatToggles,
	sitemapURLLimit int,
) (pageparse.ParsedPage, bool) {
	page, parsed := formatparse.Parse(rawURL, contentType, body, formats)
	if parsed && page.Sitemap {
		links := sitemapFollowableLinks(body, sitemapURLLimit)
		page.Links = links
		page.FollowableLinks = links
	}

	return page, parsed
}
