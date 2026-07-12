package snippetfetch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

type TextEvidence struct {
	Start int
	End   int
}

type EvidenceMatcher func(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) (TextEvidence, bool)

type enrichingSearcher struct {
	inner    searchcore.Searcher
	enricher *Enricher
	match    EvidenceMatcher
}

type enrichOutcome struct {
	snippet string
	keep    bool
}

func WithSnippetEnrichment(
	inner searchcore.Searcher,
	enricher *Enricher,
	match EvidenceMatcher,
) searchcore.Searcher {
	return enrichingSearcher{inner: inner, enricher: enricher, match: match}
}

func (s enrichingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.inner.Search(ctx, req)
	if err != nil {
		return resp, fmt.Errorf("snippet enrichment inner search: %w", err)
	}
	terms := enrichmentTerms(req)
	filtered, dropped := s.filterRemoteResults(ctx, req.Verify, terms, resp.Results)
	resp.Results = filtered
	if dropped > 0 {
		resp.TotalResults = max(len(filtered), resp.TotalResults-dropped)
	}

	return resp, nil
}

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

func (s enrichingSearcher) filterRemoteResults(
	ctx context.Context,
	verify searchcore.VerifyMode,
	terms []string,
	results []searchcore.Result,
) ([]searchcore.Result, int) {
	if len(terms) == 0 || len(results) == 0 {
		return results, 0
	}
	keep := make([]bool, len(results))
	selected := make([]int, 0, enrichLimit)
	for index, result := range results {
		if result.Source != searchcore.SourceRemote ||
			s.matches(ctx, visibleResultContent(result), terms, result.Language) ||
			s.matches(ctx, visibleResultText(result), terms, result.Language) {
			keep[index] = true
			continue
		}
		if s.enricher != nil && verify != searchcore.VerifyFalse && len(selected) < enrichLimit {
			selected = append(selected, index)
		}
	}
	outcomes := s.loadSelectedEvidence(ctx, verify, terms, results, selected)
	filtered := make([]searchcore.Result, 0, len(results))
	for index, result := range results {
		if !keep[index] && !outcomes[index].keep {
			continue
		}
		if outcomes[index].snippet != "" {
			result.Snippet = outcomes[index].snippet
		}
		filtered = append(filtered, result)
	}

	return filtered, len(results) - len(filtered)
}

func (s enrichingSearcher) loadSelectedEvidence(
	ctx context.Context,
	verify searchcore.VerifyMode,
	terms []string,
	results []searchcore.Result,
	selected []int,
) []enrichOutcome {
	outcomes := make([]enrichOutcome, len(results))
	if len(selected) == 0 {
		return outcomes
	}
	ctx, cancel := context.WithTimeout(ctx, enrichBudget)
	defer cancel()
	cacheOnly := verify == searchcore.VerifyCacheOnly
	var group sync.WaitGroup
	for _, index := range selected {
		result := results[index]
		group.Add(1)
		go func() {
			defer group.Done()
			var text string
			if cacheOnly {
				text, _ = s.enricher.cachedPageText(result.URL)
			} else {
				text, _ = s.enricher.pageText(ctx, result.URL)
			}
			outcomes[index] = s.pageOutcome(ctx, text, terms, result.Language)
		}()
	}
	group.Wait()

	return outcomes
}

func (s enrichingSearcher) pageOutcome(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) enrichOutcome {
	evidence, matched := s.matchText(ctx, text, terms, language)
	if !matched {
		return enrichOutcome{}
	}
	snippet, visible := evidenceExcerpt(text, evidence)
	if !visible || !s.matches(ctx, snippet, terms, language) {
		return enrichOutcome{}
	}

	return enrichOutcome{snippet: snippet, keep: true}
}

func (s enrichingSearcher) matches(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) bool {
	_, matched := s.matchText(ctx, text, terms, language)

	return matched
}

func (s enrichingSearcher) matchText(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) (TextEvidence, bool) {
	if s.match == nil || text == "" {
		return TextEvidence{}, false
	}

	return s.match(ctx, text, terms, language)
}

func visibleResultContent(result searchcore.Result) string {
	return strings.Join([]string{result.Title, result.Snippet}, "\n")
}

func visibleResultText(result searchcore.Result) string {
	decodedURL, err := url.PathUnescape(result.URL)
	if err != nil {
		decodedURL = result.URL
	}

	return strings.Join([]string{
		visibleResultContent(result),
		result.DisplayURL,
		decodedURL,
	}, "\n")
}
