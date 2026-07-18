package searchlocal

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func (s localSearcher) DocumentPassage(
	ctx context.Context,
	req searchcore.DocumentPassageRequest,
) (searchcore.DocumentPassage, bool, error) {
	return coreDocumentPassage(ctx, s.index, req)
}

func (s pageEvidenceSearcher) DocumentPassage(
	ctx context.Context,
	req searchcore.DocumentPassageRequest,
) (searchcore.DocumentPassage, bool, error) {
	source, ok := s.evidence.(searchindex.DocumentPassageSource)
	if !ok {
		return searchcore.DocumentPassage{}, false, fmt.Errorf(
			"document passage unavailable",
		)
	}

	return coreDocumentPassageFromSource(ctx, source, req)
}

func coreDocumentPassage(
	ctx context.Context,
	index searchindex.SearchIndex,
	req searchcore.DocumentPassageRequest,
) (searchcore.DocumentPassage, bool, error) {
	source, ok := index.(searchindex.DocumentPassageSource)
	if !ok {
		return searchcore.DocumentPassage{}, false, fmt.Errorf(
			"document passage unavailable",
		)
	}

	return coreDocumentPassageFromSource(ctx, source, req)
}

func coreDocumentPassageFromSource(
	ctx context.Context,
	source searchindex.DocumentPassageSource,
	req searchcore.DocumentPassageRequest,
) (searchcore.DocumentPassage, bool, error) {
	passage, found, err := source.DocumentPassage(ctx, searchindex.DocumentPassageRequest{
		DocumentID:       req.DocumentID,
		Analyzer:         req.Analyzer,
		Terms:            append([]string(nil), req.Terms...),
		Start:            req.Start,
		End:              req.End,
		SurroundingRunes: req.SurroundingRunes,
	})
	if err != nil {
		return searchcore.DocumentPassage{}, false, fmt.Errorf(
			"local document passage: %w",
			err,
		)
	}

	return searchcore.DocumentPassage{
		Text:         passage.Text,
		Start:        passage.Start,
		End:          passage.End,
		QueryMatches: coreBodyQueryMatches(passage.QueryMatches),
	}, found, nil
}
