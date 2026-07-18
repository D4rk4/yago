package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestSearchIndexQueryMatchEvidenceAnalyzerMapsStoredEvidence(t *testing.T) {
	analyzer := searchIndexQueryMatchEvidenceAnalyzer{}
	evidence, analyzedBytes, available, err := analyzer.AnalyzeQueryMatchEvidence(
		t.Context(),
		documentstore.Document{
			NormalizedURL: "https://example.test/alpha-beta",
			Title:         "Alpha report",
			ExtractedText: "alpha beta",
			Language:      "en",
		},
		[]string{"alpha", "beta"},
		searchindex.MaximumDocumentQueryEvidenceBytes,
	)
	if err != nil || !available || analyzedBytes == 0 || evidence.Analyzer != "en" ||
		len(evidence.BodyMatches) != 2 || len(evidence.FieldPositions) == 0 {
		t.Fatalf(
			"evidence=%#v bytes=%d available=%v error=%v",
			evidence,
			analyzedBytes,
			available,
			err,
		)
	}
	mapped := protocolQueryMatchRanges([]searchindex.TextQueryMatch{{Start: 1, End: 3}})
	if len(mapped) != 1 || mapped[0].Start != 1 || mapped[0].End != 3 {
		t.Fatalf("mapped ranges = %#v", mapped)
	}
}

func TestSearchIndexQueryMatchEvidenceAnalyzerReportsUnavailableAndCancellation(t *testing.T) {
	analyzer := searchIndexQueryMatchEvidenceAnalyzer{}
	_, _, available, err := analyzer.AnalyzeQueryMatchEvidence(
		t.Context(),
		documentstore.Document{ExtractedText: "alpha"},
		nil,
		1024,
	)
	if err != nil || available {
		t.Fatalf("empty requirements available=%v error=%v", available, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, available, err = analyzer.AnalyzeQueryMatchEvidence(
		ctx,
		documentstore.Document{ExtractedText: "alpha"},
		[]string{"alpha"},
		1024,
	)
	if available || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled analysis available=%v error=%v", available, err)
	}
}
