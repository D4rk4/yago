package searchindex

import bleveindex "github.com/blevesearch/bleve_index_api"

type bleveTermDocumentFrequency struct {
	documents uint64
	known     bool
}

func (frequency *bleveTermDocumentFrequency) read(
	reader bleveindex.TermFieldReader,
) uint64 {
	if !frequency.known {
		frequency.documents = reader.Count()
		frequency.known = true
	}

	return frequency.documents
}
