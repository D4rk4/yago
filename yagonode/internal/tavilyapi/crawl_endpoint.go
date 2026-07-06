package tavilyapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Paths of the Tavily-compatible crawl surfaces.
const (
	PathCrawl = "/crawl"
	PathMap   = "/map"
)

const (
	crawlDefaultDepth   = 1
	crawlMaxDepth       = 3
	crawlDefaultBreadth = 20
	crawlMaxBreadth     = 100
	crawlDefaultLimit   = 50
	crawlMaxLimit       = 200
)

// CrawledPage is one fetched page with its outgoing links, supplied by the
// node's egress-guarded fetcher.
type CrawledPage struct {
	Title string
	Text  string
	Links []string
}

// PageFetcher walks pages for /crawl and /map. A nil fetcher leaves the
// endpoints mounted but unavailable, like fetch-on-extract.
type PageFetcher interface {
	FetchPage(ctx context.Context, url string) (CrawledPage, error)
}

// CrawlRequest is the Tavily crawl/map request shape. The natural-language
// instructions field is accepted and ignored — no model interprets it here.
type CrawlRequest struct {
	URL            string   `json:"url"`
	MaxDepth       *int     `json:"max_depth,omitempty"`
	MaxBreadth     *int     `json:"max_breadth,omitempty"`
	Limit          *int     `json:"limit,omitempty"`
	Instructions   string   `json:"instructions,omitempty"`
	SelectPaths    []string `json:"select_paths,omitempty"`
	SelectDomains  []string `json:"select_domains,omitempty"`
	ExcludePaths   []string `json:"exclude_paths,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	AllowExternal  bool     `json:"allow_external,omitempty"`
	Format         string   `json:"format,omitempty"`
	IncludeFavicon bool     `json:"include_favicon,omitempty"`
}

// CrawlResponse is the Tavily crawl response envelope.
type CrawlResponse struct {
	BaseURL      string        `json:"base_url"`
	Results      []CrawlResult `json:"results"`
	ResponseTime float64       `json:"response_time"`
	RequestID    string        `json:"request_id"`
}

// CrawlResult is one crawled page.
type CrawlResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
	Favicon    string `json:"favicon,omitempty"`
}

// MapResponse is the Tavily map response envelope: discovered URLs only.
type MapResponse struct {
	BaseURL      string   `json:"base_url"`
	Results      []string `json:"results"`
	ResponseTime float64  `json:"response_time"`
	RequestID    string   `json:"request_id"`
}

type crawlEndpoint struct {
	access  SearchAccessPolicy
	fetcher PageFetcher
	now     func() time.Time
	mapOnly bool
}

// MountCrawl serves the Tavily-compatible POST /crawl and POST /map.
func MountCrawl(mux *http.ServeMux, access SearchAccessPolicy, fetcher PageFetcher) {
	mux.Handle(PathCrawl, crawlEndpoint{access: access, fetcher: fetcher, now: time.Now})
	mux.Handle(
		PathMap,
		crawlEndpoint{access: access, fetcher: fetcher, now: time.Now, mapOnly: true},
	)
}

func (e crawlEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST", id)

		return
	}
	// Crawling returns page content: the raw scope, like /extract.
	if decision := e.access.authorize(r, ScopeRaw); decision != DecisionAllow {
		writeAuthDecision(w, decision, id)

		return
	}
	if e.fetcher == nil {
		writeError(w, http.StatusServiceUnavailable, "crawl_unavailable",
			"crawl fetching is disabled on this node", id)

		return
	}
	var req CrawlRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", id)

		return
	}
	start := e.now()
	pages, baseURL, err := e.walk(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		code := "crawl_failed"
		if isBadRequest(err) {
			status = http.StatusBadRequest
			code = "invalid_crawl_request"
		}
		writeError(w, status, code, err.Error(), id)

		return
	}
	elapsed := e.now().Sub(start).Seconds()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if e.mapOnly {
		_ = json.NewEncoder(w).Encode(MapResponse{
			BaseURL: baseURL, Results: pageURLs(pages), ResponseTime: elapsed, RequestID: id,
		})

		return
	}
	_ = json.NewEncoder(w).Encode(CrawlResponse{
		BaseURL: baseURL, Results: crawlResults(req, pages), ResponseTime: elapsed, RequestID: id,
	})
}

// crawledEntry pairs a URL with its fetched page.
type crawledEntry struct {
	url  string
	page CrawledPage
}

// walk runs the bounded breadth-first crawl.
func (e crawlEndpoint) walk(ctx context.Context, req CrawlRequest) ([]crawledEntry, string, error) {
	bounds, err := crawlBounds(req)
	if err != nil {
		return nil, "", err
	}
	base, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || base.Host == "" || base.Scheme != "http" && base.Scheme != "https" {
		return nil, "", badRequest("url must be absolute http(s)")
	}
	filters, err := newCrawlFilters(req, base)
	if err != nil {
		return nil, "", err
	}

	walker := &crawlWalker{
		endpoint: e,
		bounds:   bounds,
		filters:  filters,
		seen:     map[string]bool{base.String(): true},
		queue:    []queuedPage{{url: base.String(), depth: 0}},
	}
	walker.run(ctx)

	return walker.entries, base.String(), nil
}

// queuedPage is one frontier entry of the bounded walk.
type queuedPage struct {
	url   string
	depth int
}

// crawlWalker runs the breadth-first walk under its bounds; fetch failures
// skip the page (partial results serve, matching Tavily's behavior).
type crawlWalker struct {
	endpoint crawlEndpoint
	bounds   crawlBoundsSet
	filters  crawlFilters
	seen     map[string]bool
	queue    []queuedPage
	entries  []crawledEntry
}

func (w *crawlWalker) run(ctx context.Context) {
	for len(w.queue) > 0 && len(w.entries) < w.bounds.limit {
		if ctx.Err() != nil {
			return
		}
		head := w.queue[0]
		w.queue = w.queue[1:]
		page, err := w.endpoint.fetcher.FetchPage(ctx, head.url)
		if err != nil {
			continue
		}
		w.entries = append(w.entries, crawledEntry{url: head.url, page: page})
		if head.depth < w.bounds.depth {
			w.enqueueLinks(page.Links, head.depth+1)
		}
	}
}

func (w *crawlWalker) enqueueLinks(links []string, depth int) {
	breadth := 0
	for _, link := range links {
		if breadth >= w.bounds.breadth || len(w.seen) >= w.bounds.limit*4 {
			return
		}
		if w.seen[link] || !w.filters.allows(link) {
			continue
		}
		w.seen[link] = true
		breadth++
		w.queue = append(w.queue, queuedPage{url: link, depth: depth})
	}
}

type crawlBoundsSet struct {
	depth   int
	breadth int
	limit   int
}

// crawlBounds validates and defaults the crawl budget.
func crawlBounds(req CrawlRequest) (crawlBoundsSet, error) {
	bounds := crawlBoundsSet{
		depth:   crawlDefaultDepth,
		breadth: crawlDefaultBreadth,
		limit:   crawlDefaultLimit,
	}
	if req.MaxDepth != nil {
		if *req.MaxDepth < 0 || *req.MaxDepth > crawlMaxDepth {
			return bounds, badRequest("max_depth out of range")
		}
		bounds.depth = *req.MaxDepth
	}
	if req.MaxBreadth != nil {
		if *req.MaxBreadth < 1 || *req.MaxBreadth > crawlMaxBreadth {
			return bounds, badRequest("max_breadth out of range")
		}
		bounds.breadth = *req.MaxBreadth
	}
	if req.Limit != nil {
		if *req.Limit < 1 || *req.Limit > crawlMaxLimit {
			return bounds, badRequest("limit out of range")
		}
		bounds.limit = *req.Limit
	}

	return bounds, nil
}

// crawlFilters applies the select/exclude path and domain rules.
type crawlFilters struct {
	baseHost       string
	allowExternal  bool
	selectPaths    []*regexp.Regexp
	excludePaths   []*regexp.Regexp
	selectDomains  []*regexp.Regexp
	excludeDomains []*regexp.Regexp
}

func newCrawlFilters(req CrawlRequest, base *url.URL) (crawlFilters, error) {
	filters := crawlFilters{baseHost: base.Hostname(), allowExternal: req.AllowExternal}
	var err error
	if filters.selectPaths, err = compilePatterns(req.SelectPaths, "select_paths"); err != nil {
		return crawlFilters{}, err
	}
	if filters.excludePaths, err = compilePatterns(req.ExcludePaths, "exclude_paths"); err != nil {
		return crawlFilters{}, err
	}
	if filters.selectDomains, err = compilePatterns(
		req.SelectDomains,
		"select_domains",
	); err != nil {
		return crawlFilters{}, err
	}
	if filters.excludeDomains, err = compilePatterns(
		req.ExcludeDomains,
		"exclude_domains",
	); err != nil {
		return crawlFilters{}, err
	}

	return filters, nil
}

func compilePatterns(patterns []string, field string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		expr, err := regexp.Compile(pattern)
		if err != nil {
			return nil, badRequest("invalid " + field + " pattern")
		}
		compiled = append(compiled, expr)
	}

	return compiled, nil
}

// allows applies the domain scope and the path/domain patterns to one link.
func (f crawlFilters) allows(link string) bool {
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if !f.allowExternal && !sameSite(host, f.baseHost) {
		return false
	}
	if len(f.selectDomains) > 0 && !anyMatch(f.selectDomains, host) {
		return false
	}
	if anyMatch(f.excludeDomains, host) {
		return false
	}
	if len(f.selectPaths) > 0 && !anyMatch(f.selectPaths, parsed.Path) {
		return false
	}
	if anyMatch(f.excludePaths, parsed.Path) {
		return false
	}

	return true
}

func anyMatch(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}

	return false
}

// sameSite treats www.X and X as one site, like the crawl scope rules.
func sameSite(host, baseHost string) bool {
	trim := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }

	return trim(host) == trim(baseHost)
}

func pageURLs(entries []crawledEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.url)
	}

	return urls
}

func crawlResults(req CrawlRequest, entries []crawledEntry) []CrawlResult {
	results := make([]CrawlResult, 0, len(entries))
	markdown := strings.EqualFold(strings.TrimSpace(req.Format), "markdown")
	for _, entry := range entries {
		content := entry.page.Text
		if markdown && entry.page.Title != "" {
			content = "# " + entry.page.Title + "\n\n" + content
		}
		result := CrawlResult{URL: entry.url, RawContent: content}
		if req.IncludeFavicon {
			result.Favicon = faviconURL(entry.url)
		}
		results = append(results, result)
	}

	return results
}
