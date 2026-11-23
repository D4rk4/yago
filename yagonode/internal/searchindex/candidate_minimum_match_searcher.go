package searchindex

import (
	"context"
	"sort"

	"github.com/blevesearch/bleve/v2/search"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type lexicalCandidateIdentitiesKey struct{}

type candidateMinimumMatchSearcher struct {
	*rarestTermsMinimumMatchSearcher
	candidates []bleveindex.IndexInternalID
	position   int
}

func withLexicalCandidateIdentities(
	ctx context.Context,
	candidates search.Searcher,
) context.Context {
	snapshot, complete := candidates.(*bleveLexicalCandidateInternalSearcher)
	if !complete {
		return ctx
	}

	return context.WithValue(ctx, lexicalCandidateIdentitiesKey{}, snapshot.identities)
}

func candidateLedMinimumMatch(
	ctx context.Context,
	minimumMatch *rarestTermsMinimumMatchSearcher,
) search.Searcher {
	candidates, bounded := ctx.Value(lexicalCandidateIdentitiesKey{}).([]bleveindex.IndexInternalID)
	if !bounded {
		return minimumMatch
	}

	return &candidateMinimumMatchSearcher{
		rarestTermsMinimumMatchSearcher: minimumMatch,
		candidates:                      candidates,
	}
}

func (s *candidateMinimumMatchSearcher) Next(
	ctx *search.SearchContext,
) (*search.DocumentMatch, error) {
	for s.position < len(s.candidates) {
		candidate := s.candidates[s.position]
		s.position++
		match, err := s.evaluate(ctx, candidate)
		if err != nil || match != nil {
			return match, err
		}
	}

	return nil, nil
}

func (s *candidateMinimumMatchSearcher) Advance(
	ctx *search.SearchContext,
	identity bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	remaining := s.candidates[s.position:]
	s.position += sort.Search(len(remaining), func(position int) bool {
		return remaining[position].Compare(identity) >= 0
	})

	return s.Next(ctx)
}
