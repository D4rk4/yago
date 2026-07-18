package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagoproto"
)

type searchIndexQueryMatchEvidenceAnalyzer struct{}

func (searchIndexQueryMatchEvidenceAnalyzer) AnalyzeQueryMatchEvidence(
	ctx context.Context,
	document documentstore.Document,
	requirements []string,
	byteLimit int,
) (yagoproto.QueryMatchEvidence, int, bool, error) {
	evidence, analyzedBytes, available, err := searchindex.AnalyzeDocumentQueryEvidence(
		ctx,
		document,
		requirements,
		byteLimit,
	)
	if err != nil {
		return yagoproto.QueryMatchEvidence{}, analyzedBytes, false,
			fmt.Errorf("analyze query match evidence: %w", err)
	}
	if !available {
		return yagoproto.QueryMatchEvidence{}, analyzedBytes, false, nil
	}

	return yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            evidence.Analyzer,
		RequirementOrdinals: evidence.RequirementOrdinals,
		AbsentOrdinals:      evidence.AbsentOrdinals,
		Snippet:             evidence.Snippet,
		SnippetMatches:      protocolQueryMatchRanges(evidence.SnippetMatches),
		BodyMatches:         protocolQueryMatchRanges(evidence.BodyMatches),
		FieldPositions: documentsearch.ProtocolQueryFieldPositions(
			evidence.FieldRequirementPositions,
		),
	}, analyzedBytes, true, nil
}

func protocolQueryMatchRanges(
	matches []searchindex.TextQueryMatch,
) []yagoproto.QueryMatchRange {
	mapped := make([]yagoproto.QueryMatchRange, len(matches))
	for index, match := range matches {
		mapped[index] = yagoproto.QueryMatchRange{Start: match.Start, End: match.End}
	}

	return mapped
}
