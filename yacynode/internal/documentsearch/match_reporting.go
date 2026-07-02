package documentsearch

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
)

type matchReport struct {
	totalMatchesPerTerm map[yacymodel.Hash]int
	documents           map[yacymodel.Hash]string
}

func (s searcher) reportMatches(
	ctx context.Context,
	criteria searchCriteria,
	wanted termMatches,
) (matchReport, error) {
	switch criteria.reporting.mode {
	case reportNoMatches:
		return matchReport{}, nil
	case reportTermWithMostMatches:
		return reportLargestWantedTerm(criteria, wanted), nil
	case reportRequestedTerms:
		return s.reportRequestedTerms(ctx, criteria, wanted)
	default:
		return matchReport{}, nil
	}
}

func reportLargestWantedTerm(criteria searchCriteria, wanted termMatches) matchReport {
	report := matchReport{totalMatchesPerTerm: wanted.totalMatchesPerTerm}
	if len(criteria.terms) <= 1 || len(criteria.requiredDocuments) != 0 {
		return report
	}
	term, ok := termWithMostMatches(wanted.documentsPerTerm)
	if !ok {
		return report
	}
	report.documents = map[yacymodel.Hash]string{
		term: yacymodel.EncodeSearchIndexAbstract(
			documentIdentifiers(wanted.documentsPerTerm[term]),
		),
	}

	return report
}

func (s searcher) reportRequestedTerms(
	ctx context.Context,
	criteria searchCriteria,
	wanted termMatches,
) (matchReport, error) {
	appearanceCriteria, _ := s.appearanceCriteria(ctx, criteria, nil)
	requested, err := s.documentsMatchingTerms(ctx, criteria.reporting.terms, appearanceCriteria)
	if err != nil {
		return matchReport{}, err
	}

	documents := make(map[yacymodel.Hash]string, len(criteria.reporting.terms))
	for _, term := range criteria.reporting.terms {
		documents[term] = yacymodel.EncodeSearchIndexAbstract(
			documentIdentifiers(requested.documentsPerTerm[term]),
		)
	}

	totals := wanted.totalMatchesPerTerm
	if len(criteria.terms) == 0 {
		totals = requested.totalMatchesPerTerm
	}

	return matchReport{totalMatchesPerTerm: totals, documents: documents}, nil
}

func termWithMostMatches(
	documentsPerTerm map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument,
) (yacymodel.Hash, bool) {
	var (
		selected yacymodel.Hash
		size     int
		found    bool
	)
	for term, documents := range documentsPerTerm {
		if !found || len(documents) > size ||
			len(documents) == size && compareAscending(term, selected) < 0 {
			selected = term
			size = len(documents)
			found = true
		}
	}

	return selected, found
}
