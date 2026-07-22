package documentsearch

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
)

type qualifiedDocuments struct {
	matches   map[yagomodel.Hash]matchedDocument
	resources map[yagomodel.Hash]yagomodel.URIMetadataRow
}

func (s searcher) qualifyDocuments(
	ctx context.Context,
	documents map[yagomodel.Hash]matchedDocument,
	constraints metadataConstraints,
) (qualifiedDocuments, error) {
	if !constraints.active() {
		return qualifiedDocuments{matches: documents}, nil
	}
	rows, err := s.documents.RowsByHash(ctx, documentIdentifiers(documents))
	if err != nil {
		return qualifiedDocuments{}, fmt.Errorf("rows by hash for metadata filters: %w", err)
	}
	qualified := qualifiedDocuments{
		matches:   make(map[yagomodel.Hash]matchedDocument, len(rows)),
		resources: make(map[yagomodel.Hash]yagomodel.URIMetadataRow, len(rows)),
	}
	for _, row := range rows {
		identifier, err := row.URLHash()
		if err != nil || !constraints.matches(ctx, row) {
			continue
		}
		hash := identifier.Hash()
		document, found := documents[hash]
		if !found {
			continue
		}
		qualified.matches[hash] = document
		qualified.resources[hash] = row
	}

	return qualified, nil
}

func (s searcher) resourcesForDocuments(
	ctx context.Context,
	identifiers []yagomodel.Hash,
	qualified map[yagomodel.Hash]yagomodel.URIMetadataRow,
) ([]yagomodel.URIMetadataRow, error) {
	if qualified == nil {
		rows, err := s.documents.RowsByHash(ctx, identifiers)
		if err != nil {
			return nil, fmt.Errorf("rows by hash: %w", err)
		}

		return rows, nil
	}
	rows := make([]yagomodel.URIMetadataRow, 0, len(identifiers))
	for _, identifier := range identifiers {
		if row, found := qualified[identifier]; found {
			rows = append(rows, row)
		}
	}

	return rows, nil
}
