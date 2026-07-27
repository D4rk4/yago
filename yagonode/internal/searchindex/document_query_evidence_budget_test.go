package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// A non-positive byte budget means "do not analyze", not "analyze nothing".
// The caller reads the availability flag to decide whether the evidence it got
// back describes the document; a zero or negative budget bounds every field to
// the empty string, so without the refusal the analyzer would report a
// successful analysis of a blank document and the caller would trust an
// all-absent requirement set. The refusal must also cost nothing: no bytes are
// reported as analyzed.
func TestAnalyzeDocumentQueryEvidenceRefusesNonPositiveByteBudget(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.test/target",
		Title:         "target",
		ExtractedText: "target body text",
	}
	for _, byteLimit := range []int{0, -1, -MaximumDocumentQueryEvidenceBytes} {
		evidence, analyzedBytes, available, err := AnalyzeDocumentQueryEvidence(
			t.Context(),
			document,
			[]string{"target"},
			byteLimit,
		)
		if err != nil || available || analyzedBytes != 0 {
			t.Fatalf(
				"budget %d: bytes=%d available=%v err=%v",
				byteLimit,
				analyzedBytes,
				available,
				err,
			)
		}
		if len(evidence.RequirementOrdinals) != 0 || evidence.Analyzer != "" {
			t.Fatalf("budget %d produced evidence: %#v", byteLimit, evidence)
		}
	}
}

// The smallest positive budget is still a budget: one byte of the document is
// analyzed and reported, so the refusal above is about the sign, not about
// budgets too small to be useful.
func TestAnalyzeDocumentQueryEvidenceAcceptsSmallestPositiveByteBudget(t *testing.T) {
	_, analyzedBytes, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{ExtractedText: "target body text"},
		[]string{"target"},
		1,
	)
	if err != nil || !available || analyzedBytes != 1 {
		t.Fatalf("one-byte budget bytes=%d available=%v err=%v", analyzedBytes, available, err)
	}
}
