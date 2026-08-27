package documentstore

type storedDocumentSetRead struct {
	documents []Document
	found     []bool
}

func (s storedDocumentPresenceSelection) recordDocuments(
	normalizedURLs []string,
	destination *storedDocumentSetRead,
	ordered storedDocumentSetRead,
	legacy storedDocumentSetRead,
) {
	destination.recordSelectedDocuments(
		normalizedURLs,
		s.orderedPositions,
		ordered,
	)
	destination.recordSelectedDocuments(
		normalizedURLs,
		s.legacyPositions,
		legacy,
	)
}

func (destination *storedDocumentSetRead) recordSelectedDocuments(
	normalizedURLs []string,
	positions []int,
	selected storedDocumentSetRead,
) {
	for index, position := range positions {
		if !selected.found[index] ||
			selected.documents[index].NormalizedURL != normalizedURLs[position] {
			continue
		}
		destination.documents[position] = selected.documents[index]
		destination.found[position] = true
	}
}
