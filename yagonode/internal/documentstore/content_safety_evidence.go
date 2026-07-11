package documentstore

import "math"

type SafetyRating uint8

const (
	SafetyUnknown SafetyRating = iota
	SafetyGeneral
	SafetyExplicit
)

type ContentSafetyEvidence struct {
	Rating              SafetyRating
	ExplicitProbability float64
	Confidence          float64
}

func normalizedContentSafetyEvidence(evidence ContentSafetyEvidence) ContentSafetyEvidence {
	if evidence.Rating > SafetyExplicit ||
		math.IsNaN(evidence.ExplicitProbability) ||
		math.IsInf(evidence.ExplicitProbability, 0) ||
		math.IsNaN(evidence.Confidence) ||
		math.IsInf(evidence.Confidence, 0) {
		return ContentSafetyEvidence{}
	}
	if evidence.Rating == SafetyUnknown {
		return ContentSafetyEvidence{}
	}
	evidence.ExplicitProbability = min(1, max(0, evidence.ExplicitProbability))
	evidence.Confidence = min(1, max(0, evidence.Confidence))

	return evidence
}
