package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestRelaxedCandidateRequiresOneCoherentPassage(t *testing.T) {
	req := SearchRequest{
		Query:              "alpha beta gamma delta epsilon zeta",
		Terms:              []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"},
		MinimumTermMatches: 4,
	}
	far := strings.Join([]string{
		"alpha",
		strings.Repeat("filler ", 40),
		"beta",
		strings.Repeat("filler ", 40),
		"gamma",
		strings.Repeat("filler ", 40),
		"delta",
	}, " ")
	if relaxedCandidateFound(t, req, far, SearchResult{RelaxedRank: 1}) {
		t.Fatal("distributed relaxed evidence was admitted")
	}
	if !relaxedCandidateFound(
		t,
		req,
		"alpha beta short passage gamma delta",
		SearchResult{RelaxedRank: 1},
	) {
		t.Fatal("coherent relaxed evidence was rejected")
	}
	latePassage := far + " alpha beta gamma delta"
	if !relaxedCandidateFound(t, req, latePassage, SearchResult{RelaxedRank: 1}) {
		t.Fatal("coherent passage after distributed evidence was rejected")
	}
	if !relaxedCandidateFound(t, req, far, SearchResult{StrictRank: 1, RelaxedRank: 1}) {
		t.Fatal("strict candidate was filtered by relaxed evidence")
	}
}

func TestRelaxedCandidateRequiresTwoNearbyTermsForThreeTermQuery(t *testing.T) {
	req := SearchRequest{
		Query:              "alpha beta gamma",
		Terms:              []string{"alpha", "beta", "gamma"},
		MinimumTermMatches: 2,
	}
	far := "alpha " + strings.Repeat("filler ", 24) + " gamma"
	if relaxedCandidateFound(t, req, far, SearchResult{RelaxedRank: 1}) {
		t.Fatal("distant two-of-three evidence was admitted")
	}
	if !relaxedCandidateFound(t, req, "alpha nearby gamma", SearchResult{RelaxedRank: 1}) {
		t.Fatal("nearby two-of-three evidence was rejected")
	}
}

func TestRelaxedCandidateDoesNotCombineDocumentFields(t *testing.T) {
	req := SearchRequest{
		Query:              "alpha beta gamma delta epsilon zeta",
		Terms:              []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"},
		MinimumTermMatches: 4,
	}
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/distributed",
		Title:         "alpha beta",
		ExtractedText: "gamma delta",
	}
	_, found, err := searchEvidenceResult(
		t.Context(),
		req,
		SearchResult{DocumentID: doc.NormalizedURL, RelaxedRank: 1},
		0,
		func(int) (documentstore.Document, bool, error) { return doc, true, nil },
	)
	if err != nil {
		t.Fatalf("searchEvidenceResult: %v", err)
	}
	if found {
		t.Fatal("cross-field relaxed evidence was admitted")
	}
}

func TestRelaxedCandidateRequiresExactSurfaceQuorum(t *testing.T) {
	req := SearchRequest{
		Query:   "best mouse gaming",
		Terms:   []string{"best", "mouse", "gaming"},
		Relaxed: true,
	}
	if relaxedCandidateFound(
		t,
		req,
		"best game changer",
		SearchResult{RelaxedRank: 1},
	) {
		t.Fatal("analyzer-only evidence admitted a relaxed candidate")
	}
	if !relaxedCandidateFound(
		t,
		req,
		"best mouse games",
		SearchResult{RelaxedRank: 1},
	) {
		t.Fatal("exact quorum with inflected recall was rejected")
	}
}

func TestAnalyzerCollapsedRequirementsDoNotShareOneOccurrence(t *testing.T) {
	matcher := newStoredEvidenceMatcher(
		SearchRequest{Terms: []string{"gaming", "games"}},
		"en",
	)
	evidence, err := scanStoredFieldEvidence(
		t.Context(),
		matcher,
		[]string{"game"},
		true,
	)
	if err != nil {
		t.Fatalf("scanStoredFieldEvidence: %v", err)
	}
	if len(evidence.requirementTerms["gaming"])+len(evidence.requirementTerms["games"]) != 1 {
		t.Fatalf("one analyzed occurrence covered two requirements: %#v", evidence.requirementTerms)
	}
}

func relaxedCandidateFound(
	t *testing.T,
	req SearchRequest,
	text string,
	candidate SearchResult,
) bool {
	t.Helper()
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/candidate",
		ExtractedText: text,
	}
	candidate.DocumentID = doc.NormalizedURL
	_, found, err := searchEvidenceResult(
		t.Context(),
		req,
		candidate,
		0,
		func(int) (documentstore.Document, bool, error) { return doc, true, nil },
	)
	if err != nil {
		t.Fatalf("searchEvidenceResult: %v", err)
	}

	return found
}
