package crawlseed

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/sitemap"
	"github.com/D4rk4/yago/yacycrawler/internal/weburl"
)

const maxSitemapFiles = 64

type Expander struct {
	source pagefetch.PageSource
	limit  int
}

func NewExpander(source pagefetch.PageSource, limit int) *Expander {
	return &Expander{source: source, limit: limit}
}

func (e *Expander) Expand(
	ctx context.Context,
	requests []yacycrawlcontract.CrawlRequest,
) ([]yacycrawlcontract.CrawlRequest, error) {
	out := make([]yacycrawlcontract.CrawlRequest, 0, len(requests))
	for _, request := range requests {
		mode, ok := yacycrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !ok {
			return nil, fmt.Errorf("unsupported crawl request mode %q", request.Mode)
		}
		switch mode {
		case yacycrawlcontract.CrawlRequestModeURL:
			request.Mode = mode
			out = append(out, request)
		case yacycrawlcontract.CrawlRequestModeSitemap:
			expanded, err := e.expandSitemap(ctx, request)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
		case yacycrawlcontract.CrawlRequestModeSitelist:
			expanded, err := e.expandSitelist(ctx, request)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
		}
	}
	return out, nil
}

func (e *Expander) expandSitemap(
	ctx context.Context,
	request yacycrawlcontract.CrawlRequest,
) ([]yacycrawlcontract.CrawlRequest, error) {
	if e.source == nil {
		return nil, fmt.Errorf("sitemap source is not configured")
	}
	var out []yacycrawlcontract.CrawlRequest
	queue := []yacycrawlcontract.CrawlRequest{request}
	seen := map[string]struct{}{}
	for len(queue) > 0 && len(out) < e.limit {
		if len(seen) >= maxSitemapFiles {
			break
		}
		current := queue[0]
		queue = queue[1:]
		page, err := e.fetch(ctx, current.URL)
		if err != nil {
			return nil, fmt.Errorf("fetch sitemap %q: %w", current.URL, err)
		}
		if e.alreadyFetchedSitemap(page, seen) {
			continue
		}
		doc, err := sitemap.ParseXML(page.Body, e.limit-len(out))
		if err != nil {
			return nil, fmt.Errorf("parse sitemap %q: %w", current.URL, err)
		}
		out = e.appendURLRequests(out, current, page, doc.URLs)
		queue = e.appendSitemapRequests(queue, current, page, doc.Sitemaps)
	}
	return out, nil
}

func (e *Expander) alreadyFetchedSitemap(
	page pagefetch.FetchedPage,
	seen map[string]struct{},
) bool {
	sitemapURL := page.URL.String()
	if _, duplicate := seen[sitemapURL]; duplicate {
		return true
	}
	seen[sitemapURL] = struct{}{}
	return false
}

func (e *Expander) appendURLRequests(
	out []yacycrawlcontract.CrawlRequest,
	current yacycrawlcontract.CrawlRequest,
	page pagefetch.FetchedPage,
	entries []sitemap.Entry,
) []yacycrawlcontract.CrawlRequest {
	for _, entry := range entries {
		if request, ok := e.requestFromEntry(current, page, entry); ok {
			out = append(out, request)
		}
	}
	return out
}

func (e *Expander) appendSitemapRequests(
	queue []yacycrawlcontract.CrawlRequest,
	current yacycrawlcontract.CrawlRequest,
	page pagefetch.FetchedPage,
	entries []sitemap.Entry,
) []yacycrawlcontract.CrawlRequest {
	for _, entry := range entries {
		if next, ok := e.sitemapRequestFromEntry(current, page, entry); ok {
			queue = append(queue, next)
		}
	}
	return queue
}

func (e *Expander) expandSitelist(
	ctx context.Context,
	request yacycrawlcontract.CrawlRequest,
) ([]yacycrawlcontract.CrawlRequest, error) {
	if e.source == nil {
		return nil, fmt.Errorf("sitelist source is not configured")
	}
	page, err := e.fetch(ctx, request.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch sitelist %q: %w", request.URL, err)
	}
	doc := sitemap.ParseSitelist(page.Body, e.limit)
	out := make([]yacycrawlcontract.CrawlRequest, 0, len(doc.URLs))
	for _, entry := range doc.URLs {
		if request, ok := e.requestFromEntry(request, page, entry); ok {
			request.ReferrerURL = ""
			out = append(out, request)
		}
	}
	return out, nil
}

func (e *Expander) fetch(
	ctx context.Context,
	rawURL string,
) (pagefetch.FetchedPage, error) {
	target, ok := weburl.ParseBase(rawURL)
	if !ok {
		return pagefetch.FetchedPage{}, fmt.Errorf("parse seed URL")
	}
	page, err := e.source.Fetch(ctx, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("fetch seed URL: %w", err)
	}
	return page, nil
}

func (e *Expander) requestFromEntry(
	parent yacycrawlcontract.CrawlRequest,
	page pagefetch.FetchedPage,
	entry sitemap.Entry,
) (yacycrawlcontract.CrawlRequest, bool) {
	resolved, ok := weburl.Resolve(page.URL, entry.URL)
	if !ok {
		return yacycrawlcontract.CrawlRequest{}, false
	}
	normalized, ok := weburl.Normalize(resolved.String())
	if !ok {
		return yacycrawlcontract.CrawlRequest{}, false
	}
	request := parent
	request.URL = normalized
	request.Mode = yacycrawlcontract.CrawlRequestModeURL
	request.ReferrerURL = parent.URL
	request.AnchorName = ""
	request.LastModified = entry.LastModified
	return request, true
}

func (e *Expander) sitemapRequestFromEntry(
	parent yacycrawlcontract.CrawlRequest,
	page pagefetch.FetchedPage,
	entry sitemap.Entry,
) (yacycrawlcontract.CrawlRequest, bool) {
	request, ok := e.requestFromEntry(parent, page, entry)
	if !ok {
		return yacycrawlcontract.CrawlRequest{}, false
	}
	request.Mode = yacycrawlcontract.CrawlRequestModeSitemap
	return request, true
}
