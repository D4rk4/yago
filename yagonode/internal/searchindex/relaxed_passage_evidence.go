package searchindex

import (
	"github.com/blevesearch/bleve/v2/search"
)

func (m *storedEvidenceMatcher) observeRelaxedPassage(
	latest map[int]*search.Location,
) {
	if m.relaxedPassageEvidence || m.minimumPassage <= 0 ||
		len(latest) < m.minimumPassage {
		return
	}
	m.relaxedPassageEvidence = relaxedPassageSupportsExactIdentifiers(
		latest,
		m.minimumPassage,
		m.exactIdentifierRequirements,
	)
}
