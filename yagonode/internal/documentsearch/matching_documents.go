package documentsearch

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type termMatches struct {
	documentsPerTerm    map[yagomodel.Hash]map[yagomodel.Hash]matchedDocument
	totalMatchesPerTerm map[yagomodel.Hash]int
}

func (s searcher) documentsMatchingTerms(
	ctx context.Context,
	terms []yagomodel.Hash,
	appearanceCriteria termAppearanceCriteria,
) (termMatches, error) {
	matches := termMatches{
		documentsPerTerm: make(
			map[yagomodel.Hash]map[yagomodel.Hash]matchedDocument,
			len(terms),
		),
		totalMatchesPerTerm: make(map[yagomodel.Hash]int, len(terms)),
	}
	for _, term := range terms {
		appearances, total, err := s.scanTerm(ctx, term, appearanceCriteria)
		if err != nil {
			return termMatches{}, err
		}
		matches.documentsPerTerm[term] = dedupeDocuments(appearances)
		matches.totalMatchesPerTerm[term] = total
	}

	return matches, nil
}

func dedupeDocuments(appearances []termAppearance) map[yagomodel.Hash]matchedDocument {
	documents := make(map[yagomodel.Hash]matchedDocument, len(appearances))
	for _, appearance := range appearances {
		if _, seen := documents[appearance.documentIdentifier]; seen {
			continue
		}
		documents[appearance.documentIdentifier] = matchedDocument{
			identifier:  appearance.documentIdentifier,
			occurrences: appearance.occurrences,
			termSpread:  appearance.termSpread,
		}
	}

	return documents
}

func documentsInTermOrder(
	terms []yagomodel.Hash,
	documentsPerTerm map[yagomodel.Hash]map[yagomodel.Hash]matchedDocument,
) []map[yagomodel.Hash]matchedDocument {
	ordered := make([]map[yagomodel.Hash]matchedDocument, 0, len(terms))
	for _, term := range terms {
		ordered = append(ordered, documentsPerTerm[term])
	}

	return ordered
}
