package documentsearch

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type termMatches struct {
	documentsPerTerm    map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument
	totalMatchesPerTerm map[yacymodel.Hash]int
}

func (s searcher) documentsMatchingTerms(
	ctx context.Context,
	terms []yacymodel.Hash,
	appearanceCriteria termAppearanceCriteria,
) (termMatches, error) {
	matches := termMatches{
		documentsPerTerm: make(
			map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument,
			len(terms),
		),
		totalMatchesPerTerm: make(map[yacymodel.Hash]int, len(terms)),
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

func dedupeDocuments(appearances []termAppearance) map[yacymodel.Hash]matchedDocument {
	documents := make(map[yacymodel.Hash]matchedDocument, len(appearances))
	for _, appearance := range appearances {
		if _, seen := documents[appearance.documentIdentifier]; seen {
			continue
		}
		documents[appearance.documentIdentifier] = matchedDocument{
			identifier:  appearance.documentIdentifier,
			occurrences: appearance.occurrences,
			minPosition: appearance.textPosition,
			maxPosition: appearance.textPosition,
		}
	}

	return documents
}

func documentsInTermOrder(
	terms []yacymodel.Hash,
	documentsPerTerm map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument,
) []map[yacymodel.Hash]matchedDocument {
	ordered := make([]map[yacymodel.Hash]matchedDocument, 0, len(terms))
	for _, term := range terms {
		ordered = append(ordered, documentsPerTerm[term])
	}

	return ordered
}
