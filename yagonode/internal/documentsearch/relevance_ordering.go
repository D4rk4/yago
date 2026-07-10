package documentsearch

import (
	"maps"
	"slices"

	"github.com/D4rk4/yago/yagomodel"
)

type matchedDocument struct {
	identifier  yagomodel.Hash
	occurrences uint64
	minPosition uint64
	maxPosition uint64
}

func (d matchedDocument) termSpread() uint64 {
	return d.maxPosition - d.minPosition
}

func keepDocumentsMatchingEveryTerm(
	perTerm []map[yagomodel.Hash]matchedDocument,
) map[yagomodel.Hash]matchedDocument {
	if len(perTerm) == 0 {
		return nil
	}
	matchingEvery := make(map[yagomodel.Hash]matchedDocument, len(perTerm[0]))
	maps.Copy(matchingEvery, perTerm[0])

	for _, documents := range perTerm[1:] {
		for identifier, document := range matchingEvery {
			alsoHere, ok := documents[identifier]
			if !ok {
				delete(matchingEvery, identifier)

				continue
			}
			document.occurrences += alsoHere.occurrences
			document.minPosition = min(document.minPosition, alsoHere.minPosition)
			document.maxPosition = max(document.maxPosition, alsoHere.maxPosition)
			matchingEvery[identifier] = document
		}
	}

	return matchingEvery
}

// Deliberate divergence from YaCy: documents are ordered by occurrences and term
// spread alone, not YaCy's normalized multi-factor ranking profile. Term spread is
// the span (max-min) of the query terms' text positions, where YaCy averages the
// consecutive positional gaps.
func documentsOrderedByRelevance(documents map[yagomodel.Hash]matchedDocument) []yagomodel.Hash {
	ranked := make([]matchedDocument, 0, len(documents))
	for _, document := range documents {
		ranked = append(ranked, document)
	}
	slices.SortFunc(ranked, documentRelevanceOrder)

	identifiers := make([]yagomodel.Hash, 0, len(ranked))
	for _, document := range ranked {
		identifiers = append(identifiers, document.identifier)
	}

	return identifiers
}

// documentRelevanceOrder ranks two matched documents by descending occurrences,
// then ascending term spread, then ascending identifier. Because identifiers are
// unique it is a strict total order, so the bounded top-k selection and the full
// sort agree on a single ordering down to the last tie-break.
func documentRelevanceOrder(a, b matchedDocument) int {
	if a.occurrences != b.occurrences {
		return compareDescending(a.occurrences, b.occurrences)
	}
	if a.termSpread() != b.termSpread() {
		return compareAscending(a.termSpread(), b.termSpread())
	}

	return compareAscending(a.identifier, b.identifier)
}

func documentsWithinTermSpread(
	documents map[yagomodel.Hash]matchedDocument,
	maxTermSpread int,
) map[yagomodel.Hash]matchedDocument {
	if maxTermSpread <= 0 {
		return documents
	}
	for identifier, document := range documents {
		if document.termSpread() > uint64(maxTermSpread) {
			delete(documents, identifier)
		}
	}

	return documents
}

func takeMostRelevant(identifiers []yagomodel.Hash, limit int) []yagomodel.Hash {
	if limit > 0 && len(identifiers) > limit {
		return identifiers[:limit]
	}

	return identifiers
}

func documentIdentifiers(documents map[yagomodel.Hash]matchedDocument) []yagomodel.Hash {
	identifiers := make([]yagomodel.Hash, 0, len(documents))
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
