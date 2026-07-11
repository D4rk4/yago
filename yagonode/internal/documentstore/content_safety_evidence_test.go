package documentstore

import (
	"math"
	"testing"
)

func TestNormalizedContentSafetyEvidence(t *testing.T) {
	valid := normalizedContentSafetyEvidence(ContentSafetyEvidence{
		Rating: SafetyExplicit, ExplicitProbability: 2, Confidence: -1,
	})
	if valid.Rating != SafetyExplicit || valid.ExplicitProbability != 1 || valid.Confidence != 0 {
		t.Fatalf("bounded evidence = %#v", valid)
	}
	invalid := []ContentSafetyEvidence{
		{Rating: SafetyRating(99), Confidence: 1},
		{Rating: SafetyGeneral, ExplicitProbability: math.NaN(), Confidence: 1},
		{Rating: SafetyGeneral, ExplicitProbability: math.Inf(1), Confidence: 1},
		{Rating: SafetyGeneral, Confidence: math.NaN()},
		{Rating: SafetyGeneral, Confidence: math.Inf(-1)},
		{Rating: SafetyUnknown, ExplicitProbability: 0.5, Confidence: 1},
	}
	for _, evidence := range invalid {
		if got := normalizedContentSafetyEvidence(evidence); got != (ContentSafetyEvidence{}) {
			t.Fatalf("invalid evidence normalized to %#v", got)
		}
	}
}

func TestReceivePersistsNormalizedContentSafetyEvidence(t *testing.T) {
	directory, receiver := openDocuments(t)
	url := "https://example.org/"
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		ContentSafety: ContentSafetyEvidence{
			Rating: SafetyGeneral, ExplicitProbability: -1, Confidence: 2,
		},
	}}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	doc, found, err := directory.Document(t.Context(), url)
	if err != nil || !found || doc.ContentSafety.Rating != SafetyGeneral ||
		doc.ContentSafety.ExplicitProbability != 0 || doc.ContentSafety.Confidence != 1 {
		t.Fatalf("document = %#v/%v/%v", doc, found, err)
	}
}
