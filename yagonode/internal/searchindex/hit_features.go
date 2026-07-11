package searchindex

import (
	"regexp"
	"sort"

	"github.com/blevesearch/bleve/v2/search"
)

// weightNodePattern matches a bleve term-scorer leaf message of the form
// "weight(<field>:<term>^<boost> in <doc>), product of:". The field and term
// name the contribution so that the same field:term appearing in several query
// clauses (required terms, phrase and bigram boosts all repeat it) collapses to
// one value instead of being summed several times over.
var weightNodePattern = regexp.MustCompile(`^weight\(([^:]+):(.+)\^[0-9.]+ in `)

// hitFieldTermPositions extracts the 1-based matched-term positions per field
// from a hit's bleve locations, returning nil when positions were not requested
// or none matched.
func hitFieldTermPositions(
	req SearchRequest,
	hit *search.DocumentMatch,
) map[string]map[string][]int {
	if !req.IncludePositions || len(hit.Locations) == 0 {
		return nil
	}
	out := make(map[string]map[string][]int, len(hit.Locations))
	for field, terms := range hit.Locations {
		termPositions := make(map[string][]int, len(terms))
		for term, locations := range terms {
			positions := make([]int, 0, len(locations))
			for _, location := range locations {
				// A token position is bounded by the document's token count
				// (documents are size-capped), so it always fits int.
				pos := int(location.Pos) //nolint:gosec // G115: bounded token position
				positions = append(positions, pos)
			}
			sort.Ints(positions)
			termPositions[term] = positions
		}
		out[field] = termPositions
	}

	return out
}

func hitFieldScores(req SearchRequest, hit *search.DocumentMatch) map[string]float64 {
	if (!req.Explain && !req.IncludeFieldScores) || hit.Expl == nil {
		return nil
	}
	perField := map[string]map[string]float64{}
	collectWeightNodes(hit.Expl, perField)
	if len(perField) == 0 {
		return nil
	}
	scores := make(map[string]float64, len(perField))
	for field, terms := range perField {
		for _, weight := range terms {
			scores[field] += weight
		}
	}

	return scores
}

// collectWeightNodes records each field:term weight leaf once (last write wins;
// repeats carry the same value) while descending the explanation tree.
func collectWeightNodes(node *search.Explanation, perField map[string]map[string]float64) {
	if node == nil {
		return
	}
	if match := weightNodePattern.FindStringSubmatch(node.Message); match != nil {
		field, term := match[1], match[2]
		if perField[field] == nil {
			perField[field] = map[string]float64{}
		}
		perField[field][term] = node.Value
	}
	for _, child := range node.Children {
		collectWeightNodes(child, perField)
	}
}
