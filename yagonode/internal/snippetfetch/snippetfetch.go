// Package snippetfetch gives peer search results real snippets, YaCy parity:
// a peer sends only a result's title, so without loading the page the SERP
// shows title-as-snippet and the query words the peer matched in the body stay
// invisible. Like YaCy's TextSnippet pass (SearchEvent.getSnippet), the top
// peer rows' pages are fetched — egress-guarded, bounded, concurrent, cached —
// their text is checked for every content word of the query, and a
// query-biased excerpt replaces the bare title. A fetched page missing the
// words is demoted behind every other row rather than dropped: YaCy sorts such
// results out (ERROR_NO_MATCH), but it verifies against a stream of hundreds of
// candidates, while dropping from a ten-row window turned honest-but-foreign
// pages (a Ukrainian article without the Russian query word) into an empty SERP
// that the zero-result layers never saw. A page that cannot be loaded keeps its
// row unchanged, exactly as YaCy keeps remote results whose snippet fetch
// fails.
package snippetfetch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	// enrichLimit bounds how many leading rows fetch their pages — the first
	// result page; deeper pagination keeps peer-sent titles.
	enrichLimit = 10
	// fetchConcurrency bounds parallel page loads per query.
	fetchConcurrency = 8
	// enrichBudget caps the whole enrichment pass so a slow host cannot stall
	// the SERP; rows still loading when it expires stay unchanged.
	enrichBudget = 2500 * time.Millisecond
	// cacheSize and cacheTTL bound the fetched-text cache, YaCy's
	// snippetsCache role: repeated queries and page-two visits reuse the text.
	cacheSize = 1024
	cacheTTL  = time.Hour
)

// TextFetcher loads a page and returns its visible text; implementations must
// be egress-guarded and size-bounded (the node wires extractfetch).
type TextFetcher func(ctx context.Context, rawURL string) (string, error)

type cachedText struct {
	text      string
	fetchedAt time.Time
}

// Enricher fetches peer result pages and swaps their title-as-snippet for a
// verified, query-biased excerpt of the page text. The fetched-text cache is a
// bounded map cleared wholesale when full — page texts expire by TTL anyway,
// and a whole-map reset every cacheSize distinct pages beats carrying an LRU
// dependency for a cache this small.
type Enricher struct {
	fetch TextFetcher
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]cachedText
}

// NewEnricher builds an enricher over the given fetcher; a nil fetcher yields
// a nil enricher, which disables enrichment.
func NewEnricher(fetch TextFetcher) *Enricher {
	if fetch == nil {
		return nil
	}

	return &Enricher{fetch: fetch, now: time.Now, cache: map[string]cachedText{}}
}

// WithSnippetEnrichment decorates a searcher so peer rows on the first result
// page carry verified, query-biased snippets from their fetched pages. A nil
// enricher returns the inner searcher unchanged; verify=false requests skip
// the pass, mirroring YaCy's verify toggle.
func WithSnippetEnrichment(inner searchcore.Searcher, enricher *Enricher) searchcore.Searcher {
	if enricher == nil {
		return inner
	}

	return enrichingSearcher{inner: inner, enricher: enricher}
}

type enrichingSearcher struct {
	inner    searchcore.Searcher
	enricher *Enricher
}

func (s enrichingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.inner.Search(ctx, req)
	if err != nil {
		return resp, fmt.Errorf("snippet enrichment inner search: %w", err)
	}
	if req.Verify == searchcore.VerifyFalse {
		return resp, nil
	}
	resp.Results = s.enricher.enrich(ctx, enrichmentTerms(req), resp.Results)

	return resp, nil
}

// enrichmentTerms is the query's content words; an all-stopword query keeps
// every term so something is still verified.
func enrichmentTerms(req searchcore.Request) []string {
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	if content := stopwords.ContentTerms(terms); len(content) > 0 {
		return content
	}

	return terms
}

type enrichOutcome struct {
	snippet string
	demote  bool
}

// enrich fetches the leading peer rows concurrently, swaps verified rows'
// snippets for page excerpts, and moves rows whose fetched page lacks a query
// word behind everything else. The demotion is best-effort: the lexical rerank
// above may lift a demoted row whose title carries the query words, which is
// acceptable — the invariant this pass guarantees is that verification never
// removes a row, so a window can only gain evidence, not turn empty.
func (e *Enricher) enrich(
	ctx context.Context,
	terms []string,
	results []searchcore.Result,
) []searchcore.Result {
	if len(terms) == 0 || len(results) == 0 {
		return results
	}
	ctx, cancel := context.WithTimeout(ctx, enrichBudget)
	defer cancel()

	head := min(len(results), enrichLimit)
	outcomes := make([]enrichOutcome, head)
	var wg sync.WaitGroup
	slots := make(chan struct{}, fetchConcurrency)
	for i, result := range results[:head] {
		if result.Source != searchcore.SourceRemote {
			continue
		}
		wg.Add(1)
		go func(index int, rawURL string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			outcomes[index] = e.pageOutcome(ctx, terms, rawURL)
		}(i, result.URL)
	}
	wg.Wait()

	kept := make([]searchcore.Result, 0, len(results))
	demoted := make([]searchcore.Result, 0, len(results))
	for i, result := range results {
		if i < head && outcomes[i].demote {
			slog.DebugContext(ctx, "peer snippet verification demoted result",
				slog.String("url", result.URL))
			demoted = append(demoted, result)

			continue
		}
		if i < head && outcomes[i].snippet != "" {
			result.Snippet = outcomes[i].snippet
		}
		kept = append(kept, result)
	}

	return append(kept, demoted...)
}

// pageOutcome loads one page and judges it: an unreachable page keeps its row,
// a page missing any content word is demoted, a verified page yields its
// query-biased excerpt.
func (e *Enricher) pageOutcome(
	ctx context.Context,
	terms []string,
	rawURL string,
) enrichOutcome {
	text, err := e.pageText(ctx, rawURL)
	if err != nil || text == "" {
		return enrichOutcome{}
	}
	for _, term := range terms {
		if !searchcore.TextMentionsTerm(text, term) {
			return enrichOutcome{demote: true}
		}
	}

	return enrichOutcome{snippet: queryBiasedExcerpt(text, terms)}
}

// pageText serves the page's extracted text from the cache or fetches it.
func (e *Enricher) pageText(ctx context.Context, rawURL string) (string, error) {
	e.mu.Lock()
	entry, ok := e.cache[rawURL]
	e.mu.Unlock()
	if ok && e.now().Sub(entry.fetchedAt) < cacheTTL {
		return entry.text, nil
	}
	text, err := e.fetch(ctx, rawURL)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	if len(e.cache) >= cacheSize {
		e.cache = map[string]cachedText{}
	}
	e.cache[rawURL] = cachedText{text: text, fetchedAt: e.now()}
	e.mu.Unlock()

	return text, nil
}
