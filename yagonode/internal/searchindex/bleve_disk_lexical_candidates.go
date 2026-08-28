package searchindex

import (
	"context"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

const diskLexicalCandidateMaximumDocuments = 32_768

func (b *BleveDiskIndex) searchLexicalCandidateHitPage(
	ctx context.Context,
	req SearchRequest,
	size int,
) (*bleve.SearchResult, error) {
	query := bleveSearchQuery(req, b.multilingual, b.analyzerScope)
	if b.analyzerScope && !req.Explain {
		query = bleve.NewConjunctionQuery(
			query,
			newBleveLexicalCandidateSnapshotQuery(req, b.multilingual),
		)
	}
	query = withBleveFuzzySearchDeadline(req, query)

	return b.executeRequestedHitPage(ctx, req, query, size)
}

func (b *BleveDiskIndex) executeRequestedHitPage(
	ctx context.Context,
	req SearchRequest,
	query blevequery.Query,
	size int,
) (*bleve.SearchResult, error) {
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = size
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = false
	searchRequest.Fields = storedSearchFields(req, b.storedCandidates)
	result, err := b.readSearchPage(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf(
			"search documents: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if err := bleveSearchCompletionError(ctx, result); err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}

	return result, nil
}

func bleveLexicalCandidateQuery(
	req SearchRequest,
	multilingual bool,
) blevequery.Query {
	analyzers := []string{""}
	if multilingual {
		analyzers = queryAnalyzers(queryAnalyzerText(req))
	}
	terms := distinctLexicalCandidateTerms(req)
	weights := req.Weights.orDefault()
	clauses := make([]blevequery.Query, 0, len(terms))
	for _, term := range terms {
		termAnalyzers := distinctLexicalCandidateAnalyzers(term, analyzers)
		if req.Fuzzy {
			clauses = append(
				clauses,
				fuzzyCrossFieldTermClause(term, termAnalyzers, weights),
			)
		} else {
			clauses = append(
				clauses,
				crossFieldTermClause(term, termAnalyzers, weights, 1),
			)
		}
	}
	if len(clauses) == 1 {
		return clauses[0]
	}

	return bleve.NewDisjunctionQuery(clauses...)
}

func distinctLexicalCandidateTerms(req SearchRequest) []string {
	terms := queryTermWords(req)
	distinct := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if _, found := seen[term]; found {
			continue
		}
		seen[term] = struct{}{}
		distinct = append(distinct, term)
	}
	if len(distinct) == 0 {
		distinct = append(distinct, req.Query)
	}

	return distinct
}
