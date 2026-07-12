package searchindex

import (
	"sort"
	"strings"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const maximumTermPositionsPerField = 64

type namedTermPositions struct {
	term      string
	positions []int
}

func boundedFieldTermPositions(
	req SearchRequest,
	terms search.TermLocationMap,
) map[string][]int {
	positionsByTerm := make(map[string][]int, len(terms))
	for term, locations := range terms {
		positions := make([]int, 0, len(locations))
		for _, location := range locations {
			positions = append(
				positions,
				int(location.Pos), //nolint:gosec // G115: bounded document token position
			)
		}
		sort.Ints(positions)
		positionsByTerm[term] = positions
	}
	witnesses := minimumRangeWitnesses(positionsByTerm, rankingPositionTerms(req))
	for term, positions := range positionsByTerm {
		witness, preserveWitness := witnesses[term]
		positionsByTerm[term] = boundedTermPositions(positions, witness, preserveWitness)
	}

	return positionsByTerm
}

func rankingPositionTerms(req SearchRequest) []string {
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	all := make([]string, 0, len(terms))
	content := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		all = append(all, term)
		if !stopwords.IsStopword(term) {
			content = append(content, term)
		}
	}
	if len(content) > 0 {
		return content
	}

	return all
}

func minimumRangeWitnesses(
	positionsByTerm map[string][]int,
	terms []string,
) map[string]int {
	lists := make([]namedTermPositions, 0, len(terms))
	for _, term := range terms {
		if positions := positionsByTerm[term]; len(positions) > 0 {
			lists = append(lists, namedTermPositions{term: term, positions: positions})
		}
	}
	if len(lists) == 0 {
		return nil
	}
	indexes := make([]int, len(lists))
	bestSpan := int(^uint(0) >> 1)
	best := make(map[string]int, len(lists))
	for {
		minimumList := 0
		minimumPosition := lists[0].positions[indexes[0]]
		maximumPosition := minimumPosition
		for index := 1; index < len(lists); index++ {
			position := lists[index].positions[indexes[index]]
			if position < minimumPosition {
				minimumList = index
				minimumPosition = position
			}
			maximumPosition = max(maximumPosition, position)
		}
		if span := maximumPosition - minimumPosition; span < bestSpan {
			bestSpan = span
			for index, list := range lists {
				best[list.term] = list.positions[indexes[index]]
			}
		}
		indexes[minimumList]++
		if indexes[minimumList] == len(lists[minimumList].positions) {
			return best
		}
	}
}

func boundedTermPositions(positions []int, witness int, preserveWitness bool) []int {
	if len(positions) <= maximumTermPositionsPerField {
		return positions
	}
	bounded := make([]int, maximumTermPositionsPerField)
	last := len(positions) - 1
	for index := range bounded {
		bounded[index] = positions[index*last/(maximumTermPositionsPerField-1)]
	}
	if !preserveWitness {
		return bounded
	}
	witnessIndex := sort.SearchInts(bounded, witness)
	if witnessIndex < len(bounded) && bounded[witnessIndex] == witness {
		return bounded
	}
	bounded[len(bounded)/2] = witness
	sort.Ints(bounded)

	return bounded
}
