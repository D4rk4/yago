package documentsearch

import (
	"maps"
	"slices"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type matchedDocument struct {
	identifier  yacymodel.Hash
	occurrences uint64
	termSpread  uint64
}

func keepDocumentsMatchingEveryTerm(
	perTerm []map[yacymodel.Hash]matchedDocument,
) map[yacymodel.Hash]matchedDocument {
	if len(perTerm) == 0 {
		return nil
	}
	matchingEvery := make(map[yacymodel.Hash]matchedDocument, len(perTerm[0]))
	maps.Copy(matchingEvery, perTerm[0])

	for _, documents := range perTerm[1:] {
		for identifier, document := range matchingEvery {
			alsoHere, ok := documents[identifier]
			if !ok {
				delete(matchingEvery, identifier)

				continue
			}
			document.occurrences += alsoHere.occurrences
			document.termSpread += alsoHere.termSpread
			matchingEvery[identifier] = document
		}
	}

	return matchingEvery
}

// Deliberate divergence from YaCy: documents are ordered by occurrences and term
// spread alone, not YaCy's normalized multi-factor ranking profile.
func documentsOrderedByRelevance(documents map[yacymodel.Hash]matchedDocument) []yacymodel.Hash {
	ranked := make([]matchedDocument, 0, len(documents))
	for _, document := range documents {
		ranked = append(ranked, document)
	}
	slices.SortFunc(ranked, func(a, b matchedDocument) int {
		if a.occurrences != b.occurrences {
			return compareDescending(a.occurrences, b.occurrences)
		}
		if a.termSpread != b.termSpread {
			return compareAscending(a.termSpread, b.termSpread)
		}

		return compareAscending(a.identifier, b.identifier)
	})

	identifiers := make([]yacymodel.Hash, 0, len(ranked))
	for _, document := range ranked {
		identifiers = append(identifiers, document.identifier)
	}

	return identifiers
}

func takeMostRelevant(identifiers []yacymodel.Hash, limit int) []yacymodel.Hash {
	if limit > 0 && len(identifiers) > limit {
		return identifiers[:limit]
	}

	return identifiers
}

func documentIdentifiers(documents map[yacymodel.Hash]matchedDocument) []yacymodel.Hash {
	identifiers := make([]yacymodel.Hash, 0, len(documents))
	for identifier := range documents {
		identifiers = append(identifiers, identifier)
	}

	return identifiers
}

func compareDescending[T ~uint64](a, b T) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareAscending[T ~uint64 | ~string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
