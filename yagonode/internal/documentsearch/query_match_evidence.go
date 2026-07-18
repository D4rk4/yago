package documentsearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	maximumInboundEvidenceCandidates    = 32
	maximumInboundEvidenceAnalysisBytes = 2 << 20
	maximumInboundEvidenceResponseBytes = 128 << 10
	MaximumQueryMatchEvidencePositions  = 256
	maximumInboundEvidenceDuration      = 100 * time.Millisecond
)

type queryMatchEvidenceSource struct {
	documents documentstore.DocumentDirectory
	analyzer  QueryMatchEvidenceAnalyzer
	budget    queryMatchEvidenceBudget
}

type QueryMatchEvidenceAnalyzer interface {
	AnalyzeQueryMatchEvidence(
		ctx context.Context,
		document documentstore.Document,
		requirements []string,
		byteLimit int,
	) (yagoproto.QueryMatchEvidence, int, bool, error)
}

type queryMatchEvidenceBudget struct {
	candidates    int
	analysisBytes int
	responseBytes int
	duration      time.Duration
}

func (s queryMatchEvidenceSource) resources(
	ctx context.Context,
	request yagoproto.SearchRequest,
	resources []yagomodel.URIMetadataRow,
) map[yagomodel.Hash]yagoproto.QueryMatchEvidence {
	if s.documents == nil || s.analyzer == nil ||
		request.EvidenceVersion != yagoproto.QueryMatchEvidenceVersion ||
		len(request.EvidenceTerms) == 0 || !negotiatedAnalyzerRequirementsBound(request) {
		return nil
	}
	budget := s.budget.withDefaults()
	evidenceCtx, cancel := context.WithTimeout(ctx, budget.duration)
	defer cancel()
	remainingAnalysisBytes := budget.analysisBytes
	remainingResponseBytes := budget.responseBytes
	limit := min(len(resources), budget.candidates)
	evidence := make(map[yagomodel.Hash]yagoproto.QueryMatchEvidence, limit)
	allowed := queryMatchEvidenceResourceAllowlist(request.URLs)
	for _, resource := range resources[:limit] {
		if evidenceCtx.Err() != nil || remainingAnalysisBytes == 0 || remainingResponseBytes == 0 {
			break
		}
		if !queryMatchEvidenceResourceAllowed(allowed, resource) {
			continue
		}
		item, analyzedBytes, hash, available := s.resource(
			evidenceCtx,
			request.EvidenceTerms,
			resource,
			remainingAnalysisBytes,
		)
		remainingAnalysisBytes -= analyzedBytes
		if !available {
			continue
		}
		encoded, _ := json.Marshal(item)
		wireBytes := base64.RawURLEncoding.EncodedLen(len(encoded))
		if wireBytes > remainingResponseBytes {
			continue
		}
		remainingResponseBytes -= wireBytes
		evidence[hash] = item
	}
	if len(evidence) == 0 {
		return nil
	}

	return evidence
}

func queryMatchEvidenceResourceAllowlist(
	hashes []yagomodel.Hash,
) map[yagomodel.Hash]struct{} {
	if len(hashes) == 0 {
		return nil
	}
	allowed := make(map[yagomodel.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		allowed[hash] = struct{}{}
	}

	return allowed
}

func queryMatchEvidenceResourceAllowed(
	allowed map[yagomodel.Hash]struct{},
	resource yagomodel.URIMetadataRow,
) bool {
	if allowed == nil {
		return true
	}
	hash, err := resource.URLHash()
	if err != nil {
		return false
	}
	_, found := allowed[hash.Hash()]

	return found
}

func (budget queryMatchEvidenceBudget) withDefaults() queryMatchEvidenceBudget {
	if budget.candidates <= 0 {
		budget.candidates = maximumInboundEvidenceCandidates
	}
	if budget.analysisBytes <= 0 {
		budget.analysisBytes = maximumInboundEvidenceAnalysisBytes
	}
	if budget.responseBytes <= 0 {
		budget.responseBytes = maximumInboundEvidenceResponseBytes
	}
	if budget.duration <= 0 {
		budget.duration = maximumInboundEvidenceDuration
	}

	return budget
}

func (s queryMatchEvidenceSource) resource(
	ctx context.Context,
	requirements []string,
	resource yagomodel.URIMetadataRow,
	byteLimit int,
) (yagoproto.QueryMatchEvidence, int, yagomodel.Hash, bool) {
	hash, err := resource.URLHash()
	if err != nil {
		return yagoproto.QueryMatchEvidence{}, 0, yagomodel.Hash(""), false
	}
	rawURL, err := yagomodel.DecodeWireForm(ctx, resource.Properties[yagomodel.URLMetaURL])
	if err != nil || rawURL == "" {
		return yagoproto.QueryMatchEvidence{}, 0, yagomodel.Hash(""), false
	}
	document, found, err := s.documents.Document(ctx, rawURL)
	if err != nil || !found {
		return yagoproto.QueryMatchEvidence{}, 0, yagomodel.Hash(""), false
	}
	analyzed, analyzedBytes, available, err := s.analyzer.AnalyzeQueryMatchEvidence(
		ctx,
		document,
		requirements,
		byteLimit,
	)
	if err != nil || !available {
		return yagoproto.QueryMatchEvidence{}, analyzedBytes, yagomodel.Hash(""), false
	}

	return analyzed, analyzedBytes, yagomodel.Hash(hash), true
}

func ProtocolQueryFieldPositions(
	fields map[string]map[int][]int,
) []yagoproto.QueryFieldPositions {
	return completeProtocolQueryFieldPositions(fields)
}
