package searchindex

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/scorer"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type rarestTermsMinimumMatchSearcher struct {
	clauses   []search.Searcher
	positions []*search.DocumentMatch
	reached   []bool
	rarity    []int
	frequency uint64
	drivers   int
	minimum   int
	scorer    *scorer.DisjunctionQueryScorer
	matching  []*search.DocumentMatch
	matched   []int
	begun     bool
}

func newRarestTermsMinimumMatchSearcher(
	clauses []search.Searcher,
	minimum int,
	options search.SearcherOptions,
) *rarestTermsMinimumMatchSearcher {
	frequencies := clauseDocumentFrequencies(clauses)
	searcher := &rarestTermsMinimumMatchSearcher{
		clauses:   clauses,
		positions: make([]*search.DocumentMatch, len(clauses)),
		reached:   make([]bool, len(clauses)),
		rarity:    clausesByRarity(frequencies),
		frequency: totalDocumentFrequency(frequencies),
		drivers:   len(clauses) - minimum + 1,
		minimum:   minimum,
		scorer:    scorer.NewDisjunctionQueryScorer(options),
		matching:  make([]*search.DocumentMatch, 0, len(clauses)),
		matched:   make([]int, 0, len(clauses)),
	}
	searcher.SetQueryNorm(1 / math.Sqrt(searcher.Weight()))

	return searcher
}

func clauseDocumentFrequencies(clauses []search.Searcher) []uint64 {
	frequencies := make([]uint64, len(clauses))
	for clause, searcher := range clauses {
		frequencies[clause] = searcher.Count()
	}

	return frequencies
}

func totalDocumentFrequency(frequencies []uint64) uint64 {
	var total uint64
	for _, frequency := range frequencies {
		total += frequency
	}

	return total
}

func clausesByRarity(frequencies []uint64) []int {
	order := make([]int, len(frequencies))
	for clause := range order {
		order[clause] = clause
	}
	slices.SortStableFunc(order, func(left int, right int) int {
		return cmp.Compare(frequencies[left], frequencies[right])
	})

	return order
}

func (s *rarestTermsMinimumMatchSearcher) Next(
	ctx *search.SearchContext,
) (*search.DocumentMatch, error) {
	if err := s.begin(ctx); err != nil {
		return nil, err
	}
	for {
		candidate := s.nextCandidate()
		if candidate == nil {
			return nil, nil
		}
		match, err := s.evaluate(ctx, candidate.IndexInternalID)
		if err != nil || match != nil {
			return match, err
		}
	}
}

func (s *rarestTermsMinimumMatchSearcher) Advance(
	ctx *search.SearchContext,
	id bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	if err := s.begin(ctx); err != nil {
		return nil, err
	}
	for _, clause := range s.rarity[:s.drivers] {
		position := s.positions[clause]
		if position != nil && position.IndexInternalID.Compare(id) < 0 {
			if err := s.moveClause(ctx, clause, id); err != nil {
				return nil, err
			}
		}
	}

	return s.Next(ctx)
}

func (s *rarestTermsMinimumMatchSearcher) begin(ctx *search.SearchContext) error {
	if s.begun {
		return nil
	}
	s.begun = true
	for _, clause := range s.rarity[:s.drivers] {
		if err := s.moveClause(ctx, clause, nil); err != nil {
			return err
		}
	}

	return nil
}

func (s *rarestTermsMinimumMatchSearcher) nextCandidate() *search.DocumentMatch {
	var candidate *search.DocumentMatch
	for _, clause := range s.rarity[:s.drivers] {
		position := s.positions[clause]
		if position != nil &&
			(candidate == nil || position.IndexInternalID.Compare(candidate.IndexInternalID) < 0) {
			candidate = position
		}
	}

	return candidate
}

func (s *rarestTermsMinimumMatchSearcher) evaluate(
	ctx *search.SearchContext,
	id bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	s.matching = s.matching[:0]
	s.matched = s.matched[:0]
	for visited, clause := range s.rarity {
		if len(s.matching)+len(s.rarity)-visited < s.minimum {
			break
		}
		position := s.positions[clause]
		if !s.reached[clause] || position != nil && position.IndexInternalID.Compare(id) < 0 {
			if err := s.moveClause(ctx, clause, id); err != nil {
				return nil, err
			}
			position = s.positions[clause]
		}
		if position != nil && position.IndexInternalID.Equals(id) {
			s.matching = append(s.matching, position)
			s.matched = append(s.matched, clause)
		}
	}
	var match *search.DocumentMatch
	if len(s.matching) >= s.minimum {
		match = s.scorer.Score(ctx, s.matching, len(s.matching), len(s.clauses))
	}
	for _, clause := range s.matched {
		if s.positions[clause] == match {
			s.positions[clause] = nil
		}
		if err := s.moveClause(ctx, clause, nil); err != nil {
			return nil, err
		}
	}

	return match, nil
}

func (s *rarestTermsMinimumMatchSearcher) moveClause(
	ctx *search.SearchContext,
	clause int,
	target bleveindex.IndexInternalID,
) error {
	if s.positions[clause] != nil {
		ctx.DocumentMatchPool.Put(s.positions[clause])
	}
	var position *search.DocumentMatch
	var err error
	if target == nil {
		position, err = s.clauses[clause].Next(ctx)
	} else {
		position, err = s.clauses[clause].Advance(ctx, target)
	}
	if err != nil {
		s.positions[clause] = nil

		return fmt.Errorf("move minimum-match term clause: %w", err)
	}
	s.positions[clause] = position
	s.reached[clause] = true

	return nil
}

func (s *rarestTermsMinimumMatchSearcher) Weight() float64 {
	weight := 0.0
	for _, clause := range s.clauses {
		weight += clause.Weight()
	}

	return weight
}

func (s *rarestTermsMinimumMatchSearcher) SetQueryNorm(norm float64) {
	for _, clause := range s.clauses {
		clause.SetQueryNorm(norm)
	}
}

func (s *rarestTermsMinimumMatchSearcher) Count() uint64 {
	return s.frequency
}

func (s *rarestTermsMinimumMatchSearcher) Min() int {
	return s.minimum
}

func (s *rarestTermsMinimumMatchSearcher) Size() int {
	size := 0
	for _, clause := range s.clauses {
		size += clause.Size()
	}

	return size
}

func (s *rarestTermsMinimumMatchSearcher) DocumentMatchPoolSize() int {
	size := len(s.clauses)
	for _, clause := range s.clauses {
		size += clause.DocumentMatchPoolSize()
	}

	return size
}

func (s *rarestTermsMinimumMatchSearcher) Close() error {
	var first error
	for _, clause := range s.clauses {
		if err := clause.Close(); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return fmt.Errorf("close minimum-match term clause: %w", first)
	}

	return nil
}
