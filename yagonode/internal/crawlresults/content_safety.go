package crawlresults

import (
	"math"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func contentSafetyFromIngest(
	doc yagocrawlcontract.DocumentIngest,
	classifier ContentSafetyClassifier,
) documentstore.ContentSafetyEvidence {
	evidence := contentsafety.RecognizeStructured(contentsafety.StructuredLabels{
		RatingValues:   doc.SafetyLabels.RatingValues,
		FamilyFriendly: doc.SafetyLabels.FamilyFriendly,
	})
	if evidence.Rating == contentsafety.Unknown && classifier != nil {
		evidence = classifier.Classify(strings.TrimSpace(doc.Title + " " + doc.ExtractedText))
	}

	return documentSafetyEvidence(evidence)
}

func documentSafetyEvidence(
	evidence contentsafety.Evidence,
) documentstore.ContentSafetyEvidence {
	if math.IsNaN(evidence.ExplicitProbability) ||
		math.IsInf(evidence.ExplicitProbability, 0) ||
		math.IsNaN(evidence.Confidence) ||
		math.IsInf(evidence.Confidence, 0) {
		return documentstore.ContentSafetyEvidence{}
	}
	rating := documentstore.SafetyGeneral
	if evidence.Rating == contentsafety.Explicit {
		rating = documentstore.SafetyExplicit
	} else if evidence.Rating != contentsafety.General {
		return documentstore.ContentSafetyEvidence{}
	}

	return documentstore.ContentSafetyEvidence{
		Rating:              rating,
		ExplicitProbability: min(1, max(0, evidence.ExplicitProbability)),
		Confidence:          min(1, max(0, evidence.Confidence)),
	}
}
