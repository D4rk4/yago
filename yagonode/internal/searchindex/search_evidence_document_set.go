package searchindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (b *BleveDiskIndex) searchEvidenceFromDocumentSet(
	ctx context.Context,
	req SearchRequest,
	results []SearchResult,
	directory documentstore.DocumentSetDirectory,
) ([]SearchResult, error) {
	limit := min(maximumSearchEvidenceResults, len(results))
	if limit == 0 {
		return []SearchResult{}, nil
	}
	identities := make([]string, limit)
	for index := range identities {
		identities[index] = results[index].DocumentID
	}
	documents, found, err := directory.Documents(ctx, identities)
	if err != nil {
		if !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("load stored search evidence: %w", err)
		}

		return searchEvidenceResults(
			ctx,
			req,
			results,
			func(int) (documentstore.Document, bool, error) {
				return documentstore.Document{}, false, fmt.Errorf(
					"load stored search evidence: %w",
					err,
				)
			},
		)
	}
	if len(documents) != limit || len(found) != limit {
		return nil, fmt.Errorf(
			"document set results = %d/%d, want %d",
			len(documents),
			len(found),
			limit,
		)
	}
	enriched, err := searchEvidenceResults(
		ctx,
		req,
		results,
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], found[index], nil
		},
	)

	return enriched, err
}
