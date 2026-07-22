package documentsearch

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type searcher struct {
	index          rwi.PostingIndex
	documents      urlmeta.URLDirectory
	matchesPerTerm int
}

type searchResult struct {
	resources                         []yagomodel.URIMetadataRow
	topics                            []string
	totalDocumentsMatchingEveryTerm   int
	searchDuration                    time.Duration
	totalMatchesPerTerm               map[yagomodel.Hash]int
	documentsMatchingEachReportedTerm map[yagomodel.Hash]string
}

func (s searcher) search(ctx context.Context, criteria searchCriteria) (searchResult, error) {
	start := time.Now()

	appearanceCriteria, err := s.appearanceCriteria(ctx, criteria, criteria.excludedTerms)
	if err != nil {
		return searchResult{}, err
	}
	wanted, err := s.documentsMatchingTerms(
		ctx, criteria.terms, appearanceCriteria, !criteria.allowEarlyTermination,
	)
	if err != nil {
		return searchResult{}, err
	}

	matchingEveryTerm := documentsWithinTermSpread(
		keepDocumentsMatchingEveryTerm(
			documentsInTermOrder(criteria.terms, wanted.documentsPerTerm),
		),
		criteria.maxTermSpread,
	)
	qualified, err := s.qualifyDocuments(ctx, matchingEveryTerm, criteria.metadata)
	if err != nil {
		return searchResult{}, err
	}
	mostRelevant := mostRelevantDocuments(qualified.matches, criteria.maxResults)
	resources, err := s.resourcesForDocuments(ctx, mostRelevant, qualified.resources)
	if err != nil {
		return searchResult{}, err
	}
	resources = resourcesWithWordReferences(resources, qualified.matches)

	report, err := s.reportMatches(ctx, criteria, wanted)
	if err != nil {
		return searchResult{}, err
	}

	return searchResult{
		resources:                         resources,
		topics:                            resultTopics(ctx, resources, criteria.terms),
		totalDocumentsMatchingEveryTerm:   len(qualified.matches),
		searchDuration:                    time.Since(start),
		totalMatchesPerTerm:               report.totalMatchesPerTerm,
		documentsMatchingEachReportedTerm: report.documents,
	}, nil
}
