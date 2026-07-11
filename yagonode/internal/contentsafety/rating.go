package contentsafety

import (
	"math"
	"strings"
)

type Rating uint8

const (
	Unknown Rating = iota
	General
	Explicit
)

const RTARatingToken = "RTA-" + "5042-1996-1400-1577-" + "RTA"

type Evidence struct {
	Rating              Rating  `json:"rating"`
	ExplicitProbability float64 `json:"explicit_probability"`
	Confidence          float64 `json:"confidence"`
}

type StructuredLabels struct {
	RatingValues   []string
	FamilyFriendly *bool
}

func RecognizeStructured(labels StructuredLabels) Evidence {
	for _, value := range labels.RatingValues {
		if strings.EqualFold(strings.TrimSpace(value), "adult") || value == RTARatingToken {
			return certainExplicitEvidence()
		}
	}
	if labels.FamilyFriendly == nil {
		return Evidence{Rating: Unknown}
	}
	if !*labels.FamilyFriendly {
		return certainExplicitEvidence()
	}

	return Evidence{Rating: Unknown}
}

func certainExplicitEvidence() Evidence {
	return Evidence{Rating: Explicit, ExplicitProbability: 1, Confidence: 1}
}

func probabilityEvidence(probability float64) Evidence {
	rating := General
	if probability >= 0.5 {
		rating = Explicit
	}

	return Evidence{
		Rating:              rating,
		ExplicitProbability: probability,
		Confidence:          math.Abs(2*probability - 1),
	}
}
