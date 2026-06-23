package documentsearch

import (
	"context"
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type searcher struct {
	index          rwi.PostingScanner
	documents      urlmeta.URLDirectory
	matchesPerTerm int
}

type searchResult struct {
	resources                         []yacymodel.URIMetadataRow
	topics                            []string
	totalDocumentsMatchingEveryTerm   int
	searchDuration                    time.Duration
	totalMatchesPerTerm               map[yacymodel.Hash]int
	documentsMatchingEachReportedTerm map[yacymodel.Hash]string
}

func (s searcher) search(ctx context.Context, criteria searchCriteria) (searchResult, error) {
	start := time.Now()

	appearanceCriteria, err := s.appearanceCriteria(ctx, criteria, criteria.excludedTerms)
	if err != nil {
		return searchResult{}, err
	}
	wanted, err := s.documentsMatchingTerms(ctx, criteria.terms, appearanceCriteria)
	if err != nil {
		return searchResult{}, err
	}

	matchingEveryTerm := keepDocumentsMatchingEveryTerm(
		documentsInTermOrder(criteria.terms, wanted.documentsPerTerm),
	)
	mostRelevant := takeMostRelevant(
		documentsOrderedByRelevance(matchingEveryTerm),
		criteria.maxResults,
	)
	resources, err := s.documents.RowsByHash(ctx, mostRelevant)
	if err != nil {
		return searchResult{}, fmt.Errorf("rows by hash: %w", err)
	}

	report, err := s.reportMatches(ctx, criteria, wanted)
	if err != nil {
		return searchResult{}, err
	}

	return searchResult{
		resources:                         resources,
		topics:                            resultTopics(ctx, resources, criteria.terms),
		totalDocumentsMatchingEveryTerm:   len(matchingEveryTerm),
		searchDuration:                    time.Since(start),
		totalMatchesPerTerm:               report.totalMatchesPerTerm,
		documentsMatchingEachReportedTerm: report.documents,
	}, nil
}
