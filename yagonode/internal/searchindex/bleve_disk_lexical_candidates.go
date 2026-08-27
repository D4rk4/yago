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
	if !b.analyzerScope || req.Explain {
		return b.executeRequestedHitPage(ctx, req, query, size)
	}
	candidates, complete, err := b.completeLexicalCandidateSet(ctx, req)
	if err != nil {
		return nil, err
	}
	if !complete {
		return b.executeRequestedHitPage(ctx, req, query, size)
	}
	if len(candidates.Hits) == 0 {
		return candidates, nil
	}
	identities := make([]string, len(candidates.Hits))
	for position, hit := range candidates.Hits {
		identities[position] = hit.ID
	}
	identityQuery := bleve.NewDocIDQuery(identities)
	identityQuery.SetBoost(0)

	return b.executeRequestedHitPage(
		ctx,
		req,
		bleve.NewConjunctionQuery(query, identityQuery),
		size,
	)
}

func (b *BleveDiskIndex) completeLexicalCandidateSet(
	ctx context.Context,
	req SearchRequest,
) (*bleve.SearchResult, bool, error) {
	return completeLexicalCandidateSetWithin(
		ctx,
		b.alias,
		bleveLexicalCandidateQuery(req, b.multilingual),
		diskLexicalCandidateMaximumDocuments,
	)
}

func completeLexicalCandidateSetWithin(
	ctx context.Context,
	index bleve.Index,
	query blevequery.Query,
	maximumDocuments int,
) (*bleve.SearchResult, bool, error) {
	request := bleve.NewSearchRequest(query)
	request.Size = maximumDocuments + 1
	request.Score = bleve.ScoreNone
	result, err := index.SearchInContext(ctx, request)
	if err != nil {
		return nil, false, fmt.Errorf(
			"collect lexical candidates: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if err := bleveSearchCompletionError(ctx, result); err != nil {
		return nil, false, fmt.Errorf("collect lexical candidates: %w", err)
	}
	if len(result.Hits) > maximumDocuments ||
		result.Total > uint64(len(result.Hits)) {
		return nil, false, nil
	}

	return result, true, nil
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
	result, err := b.alias.SearchInContext(ctx, searchRequest)
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
		if req.Fuzzy {
			clauses = append(clauses, fuzzyCrossFieldTermClause(term, analyzers, weights))
		} else {
			clauses = append(clauses, crossFieldTermClause(term, analyzers, weights, 1))
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
