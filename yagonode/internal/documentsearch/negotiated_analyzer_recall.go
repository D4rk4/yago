package documentsearch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	maximumNegotiatedAnalyzerCandidates    = 32
	maximumNegotiatedAnalyzerDuration      = 100 * time.Millisecond
	msgNegotiatedAnalyzerRecallUnavailable = "negotiated analyzer recall unavailable"
)

type negotiatedAnalyzerRecallSource struct {
	searcher  searchcore.Searcher
	documents urlmeta.URLDirectory
	duration  time.Duration
}

func (s negotiatedAnalyzerRecallSource) merge(
	ctx context.Context,
	request yagoproto.SearchRequest,
	current searchResult,
) (searchResult, error) {
	if s.searcher == nil || s.documents == nil || !negotiatedAnalyzerRecallEligible(request) {
		return current, nil
	}
	duration := s.duration
	if duration <= 0 {
		duration = maximumNegotiatedAnalyzerDuration
	}
	recallContext, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	limit := receiverSearchCount(request.Count)
	candidates, err := s.searcher.Search(
		recallContext,
		negotiatedAnalyzerSearchRequest(request, limit),
	)
	if err != nil {
		return current, fmt.Errorf("search analyzer index: %w", err)
	}
	rows, err := s.rows(recallContext, request.URLs, candidates.Results)
	if err != nil {
		return current, err
	}
	current.resources = mergeAnalyzerRows(rows, current.resources, limit)
	current.totalDocumentsMatchingEveryTerm = max(
		current.totalDocumentsMatchingEveryTerm,
		candidates.TotalResults,
		len(current.resources),
	)

	return current, nil
}

func negotiatedAnalyzerRecallEligible(request yagoproto.SearchRequest) bool {
	return request.EvidenceVersion == yagoproto.QueryMatchEvidenceVersion &&
		len(request.EvidenceTerms) > 0 && len(request.Exclude) == 0 &&
		len(request.Abstracts.Hashes()) == 0 && request.SiteHash == "" &&
		request.Constraint == "" && request.Protocol == "" &&
		negotiatedAnalyzerRequirementsBound(request)
}

func negotiatedAnalyzerSearchRequest(
	request yagoproto.SearchRequest,
	resultLimit int,
) searchcore.Request {
	operators := parseQueryOperators(request.Modifier)
	contentDomain := ""
	if request.StrictContentDom {
		contentDomain = string(request.ContentDom)
	}
	siteHost := firstNonempty(request.SiteHost, operators.SiteHost)

	return searchcore.Request{
		Query:         strings.Join(request.EvidenceTerms, " "),
		Terms:         append([]string(nil), request.EvidenceTerms...),
		Limit:         min(maximumNegotiatedAnalyzerCandidates, max(resultLimit*2, resultLimit)),
		Source:        searchcore.SourceLocal,
		Language:      operators.Language,
		SiteHost:      siteHost,
		ContentDomain: searchcore.ContentDomain(contentDomain),
		FileType:      request.FileType,
	}
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}

func (s negotiatedAnalyzerRecallSource) rows(
	ctx context.Context,
	required []yagomodel.Hash,
	results []searchcore.Result,
) ([]yagomodel.URIMetadataRow, error) {
	allowed := make(map[yagomodel.Hash]struct{}, len(required))
	for _, hash := range required {
		allowed[hash] = struct{}{}
	}
	hashes := make([]yagomodel.Hash, 0, len(results))
	seen := make(map[yagomodel.Hash]struct{}, len(results))
	for _, result := range results {
		hash, _ := yagomodel.HashURL(result.URL)
		value := hash.Hash()
		if len(allowed) > 0 {
			if _, found := allowed[value]; !found {
				continue
			}
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		hashes = append(hashes, value)
	}
	rows, err := s.documents.RowsByHash(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("load analyzer url metadata: %w", err)
	}

	return rows, nil
}

func mergeAnalyzerRows(
	analyzerRows []yagomodel.URIMetadataRow,
	legacyRows []yagomodel.URIMetadataRow,
	limit int,
) []yagomodel.URIMetadataRow {
	merged := make([]yagomodel.URIMetadataRow, 0, min(limit, len(analyzerRows)+len(legacyRows)))
	seen := make(map[yagomodel.Hash]struct{}, cap(merged))
	for _, rows := range [][]yagomodel.URIMetadataRow{analyzerRows, legacyRows} {
		for _, row := range rows {
			hash, err := row.URLHash()
			if err != nil {
				continue
			}
			value := yagomodel.Hash(hash)
			if _, found := seen[value]; found {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, row)
			if len(merged) == limit {
				return merged
			}
		}
	}

	return merged
}
