package websearch

import (
	"context"
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

// DDGSConfig configures the keyless DDGS-family metasearch provider. Client must
// be an egress-guarded HTTP client so outbound requests cannot reach private
// addresses (see ADR-0013). Backend selects the engine list; "auto" excludes
// DuckDuckGo because it aggressively rate-limits automated queries (ADR-0021).
type DDGSConfig struct {
	Client     *http.Client
	Backend    string
	MaxResults int
	SafeSearch string
	Timeout    time.Duration
	CacheTTL   time.Duration
	CacheMax   int
	Now        func() time.Time
}

// DDGSProvider is a best-effort metasearch client over keyless public search
// engines. It caches responses briefly and backs off on rate limiting,
// degrading to an empty result rather than failing the caller's search.
type DDGSProvider struct {
	client     *http.Client
	engines    []engine
	safeSearch string
	maxResults int
	timeout    time.Duration
	now        func() time.Time
	cache      *queryCache

	mu           sync.Mutex
	backoffUntil time.Time
	backoff      time.Duration
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
		cache:      newQueryCache(config.CacheTTL, cacheMax, now),
	}
}

func (p *DDGSProvider) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if cached, ok := p.cache.get(query); ok {
		return capResults(cached, p.limit(limit)), nil
	}
	if p.backedOff() {
		return nil, nil
	}
	results, rateLimited, err := p.query(ctx, query)
	if rateLimited {
		p.recordBackoff()

		return nil, nil
	}
	p.resetBackoff()
	if err != nil {
		return nil, err
	}
	p.cache.put(query, results)

	return capResults(results, p.limit(limit)), nil
}

func (p *DDGSProvider) query(ctx context.Context, query string) ([]Result, bool, error) {
	var lastErr error
	allRateLimited := true
	for _, backend := range p.engines {
		results, rateLimited, err := p.fetch(ctx, backend, query)
		if rateLimited {
			continue
		}
		allRateLimited = false
		if err != nil {
			lastErr = err

			continue
		}
		if len(results) > 0 {
			return results, false, nil
		}
	}
	if allRateLimited {
		return nil, true, nil
	}

	return nil, false, lastErr
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

func (p *DDGSProvider) backedOff() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.now().Before(p.backoffUntil)
}

func (p *DDGSProvider) recordBackoff() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.backoff == 0 {
		p.backoff = minBackoff
	} else {
		p.backoff *= 2
	}
	if p.backoff > maxBackoff {
		p.backoff = maxBackoff
	}
	p.backoffUntil = p.now().Add(p.backoff)
}

func (p *DDGSProvider) resetBackoff() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backoff = 0
	p.backoffUntil = time.Time{}
}

func capResults(results []Result, limit int) []Result {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}

	return results
}
