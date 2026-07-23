package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	ddgsUserAgent   = "Mozilla/5.0 (compatible; yago-websearch/1.0; +https://github.com/D4rk4/yago)"
	maxResultBytes  = 2 << 20
	minBackoff      = 30 * time.Second
	maxBackoff      = 15 * time.Minute
	defaultCacheMax = 256
)

type DDGSConfig struct {
	Client     *http.Client
	Backend    string
	MaxResults int
	SafeSearch string
	Timeout    time.Duration
	CacheTTL   time.Duration
	CacheMax   int
	Now        func() time.Time
	Accept     func(query string, results []Result) []Result
}

// DDGSProvider is a best-effort metasearch client over keyless public search
// engines. It caches responses briefly and backs off on rate limiting,
// returning an operational error that the search decorator exposes as a
// partial web failure without discarding primary results.
type DDGSProvider struct {
	client     *http.Client
	engines    []engine
	safeSearch string
	maxResults int
	timeout    time.Duration
	now        func() time.Time
	cache      *queryCache
	accept     func(query string, results []Result) []Result
	admission  *engineFetchAdmission

	mu                  sync.Mutex
	backoffs            map[string]*engineBackoff
	unavailableReported bool
}

type engineBackoff struct {
	until   time.Time
	backoff time.Duration
}

func NewDDGSProvider(config DDGSConfig) *DDGSProvider {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	cacheMax := config.CacheMax
	if cacheMax <= 0 {
		cacheMax = defaultCacheMax
	}

	return &DDGSProvider{
		client:     client,
		engines:    backendsFor(config.Backend),
		safeSearch: config.SafeSearch,
		maxResults: config.MaxResults,
		timeout:    config.Timeout,
		now:        now,
		cache:      newQueryCache(config.CacheTTL, cacheMax, defaultCacheBytes, now),
		accept:     config.Accept,
		admission:  processEngineFetchAdmission,
		backoffs:   map[string]*engineBackoff{},
	}
}

func (p *DDGSProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return p.searchProviderQuery(ctx, newProviderQuery(query), limit)
}

func (p *DDGSProvider) searchProviderQuery(
	ctx context.Context,
	query providerQuery,
	limit int,
) ([]Result, error) {
	query.outboundText = strings.TrimSpace(query.outboundText)
	if query.outboundText == "" {
		return nil, nil
	}
	if cached, ok := p.cache.get(query.cacheIdentity); ok {
		return capResults(cached, p.limit(limit)), nil
	}
	results, rateLimited, err := p.query(ctx, query)
	if rateLimited {
		p.reportUnavailable(ctx, errWebSearchEnginesUnavailable)

		return nil, errWebSearchEnginesUnavailable
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			p.reportUnavailable(ctx, err)
		}

		return nil, err
	}
	p.markAvailable()
	results = normalizeResults(results, p.cachedResultLimit())
	// An empty answer is a miss, not an answer: engines rate-limit and bot-wall
	// intermittently, and caching the miss would pin the failure for the whole
	// TTL while the next attempt might succeed.
	if len(results) > 0 {
		p.cache.put(query.cacheIdentity, results)
	}

	return capResults(results, p.limit(limit)), nil
}

func (p *DDGSProvider) query(
	ctx context.Context,
	query providerQuery,
) ([]Result, bool, error) {
	return newEngineRace(p, ctx, query).run()
}

func (p *DDGSProvider) fetch(
	ctx context.Context,
	backend engine,
	query string,
) ([]Result, bool, error) {
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	endpoint := backend.endpoint + "?" + backend.params(query, p.safeSearch).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build %s request: %w", backend.name, err)
	}
	req.Header.Set("User-Agent", ddgsUserAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("%s request: %w", backend.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("%s status %d", backend.name, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResultBytes))
	if err != nil {
		return nil, false, fmt.Errorf("read %s body: %w", backend.name, err)
	}
	results, err := backend.parse(body)
	if err != nil {
		return nil, false, err
	}

	return results, false, nil
}

func (p *DDGSProvider) limit(callerLimit int) int {
	if p.maxResults > 0 && (callerLimit <= 0 || p.maxResults < callerLimit) {
		return p.maxResults
	}

	return callerLimit
}

func (p *DDGSProvider) backedOff(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.backoffs[name]

	return ok && p.now().Before(entry.until)
}

func (p *DDGSProvider) recordBackoff(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.backoffs[name]
	if !ok {
		entry = &engineBackoff{}
		p.backoffs[name] = entry
	}
	if entry.backoff == 0 {
		entry.backoff = minBackoff
	} else {
		entry.backoff *= 2
	}
	if entry.backoff > maxBackoff {
		entry.backoff = maxBackoff
	}
	entry.until = p.now().Add(entry.backoff)
}

func (p *DDGSProvider) resetBackoff(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.backoffs, name)
}

func capResults(results []Result, limit int) []Result {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}

	return results
}
