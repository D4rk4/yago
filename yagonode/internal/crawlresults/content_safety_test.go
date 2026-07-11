package crawlresults

import (
	"math"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type safetyClassifierScript struct {
	evidence contentsafety.Evidence
	calls    int
	text     string
}

func (s *safetyClassifierScript) Classify(text string) contentsafety.Evidence {
	s.calls++
	s.text = text

	return s.evidence
}

func TestContentSafetyFromIngestPrefersBlockingStructuredEvidence(t *testing.T) {
	classifier := &safetyClassifierScript{evidence: contentsafety.Evidence{
		Rating: contentsafety.General, Confidence: 1,
	}}
	doc := yagocrawlcontract.DocumentIngest{
		Title: "Title", ExtractedText: "body",
		SafetyLabels: yagocrawlcontract.SafetyLabels{RatingValues: []string{"adult"}},
	}
	evidence := contentSafetyFromIngest(doc, classifier)
	if evidence.Rating != documentstore.SafetyExplicit ||
		evidence.ExplicitProbability != 1 || evidence.Confidence != 1 || classifier.calls != 0 {
		t.Fatalf("evidence/classifier = %#v/%#v", evidence, classifier)
	}

	friendly := true
	doc.SafetyLabels = yagocrawlcontract.SafetyLabels{FamilyFriendly: &friendly}
	evidence = contentSafetyFromIngest(doc, classifier)
	if evidence.Rating != documentstore.SafetyGeneral ||
		evidence.ExplicitProbability != 0 || classifier.calls != 1 {
		t.Fatalf("family-friendly evidence = %#v", evidence)
	}
}

func TestContentSafetyFromIngestUsesOptionalClassifier(t *testing.T) {
	doc := yagocrawlcontract.DocumentIngest{Title: " Title ", ExtractedText: " body "}
	if got := contentSafetyFromIngest(doc, nil); got != (documentstore.ContentSafetyEvidence{}) {
		t.Fatalf("unknown evidence = %#v", got)
	}
	classifier := &safetyClassifierScript{evidence: contentsafety.Evidence{
		Rating: contentsafety.Explicit, ExplicitProbability: 1.2, Confidence: -0.2,
	}}
	evidence := contentSafetyFromIngest(doc, classifier)
	if classifier.calls != 1 || classifier.text != "Title   body" ||
		evidence.Rating != documentstore.SafetyExplicit ||
		evidence.ExplicitProbability != 1 || evidence.Confidence != 0 {
		t.Fatalf("classified evidence = %#v/%#v", evidence, classifier)
	}
	consumer := &IngestConsumer{}
	consumer.UseContentSafetyClassifier(nil)
	if consumer.safety != nil {
		t.Fatal("nil classifier changed consumer")
	}
	consumer.UseContentSafetyClassifier(classifier)
	if consumer.safety != classifier {
		t.Fatal("classifier was not installed")
	}
	converted := documentFromIngestWithSafety(doc, classifier)
	if converted.ContentSafety.Rating != documentstore.SafetyExplicit {
		t.Fatalf("converted document = %#v", converted)
	}
}

func TestContentSafetyFromIngestClassifiesPositivePublisherLabel(t *testing.T) {
	friendly := true
	classifier := &safetyClassifierScript{evidence: contentsafety.Evidence{
		Rating: contentsafety.Explicit, ExplicitProbability: 0.9, Confidence: 0.8,
	}}
	doc := yagocrawlcontract.DocumentIngest{
		Title: "Publisher title", ExtractedText: "classified body",
		SafetyLabels: yagocrawlcontract.SafetyLabels{FamilyFriendly: &friendly},
	}
	evidence := contentSafetyFromIngest(doc, classifier)
	if classifier.calls != 1 ||
		evidence.Rating != documentstore.SafetyExplicit ||
		evidence.ExplicitProbability != 0.9 || evidence.Confidence != 0.8 {
		t.Fatalf("evidence/classifier = %#v/%#v", evidence, classifier)
	}
}

func TestDocumentSafetyEvidenceRejectsInvalidValuesAndRatings(t *testing.T) {
	invalid := []contentsafety.Evidence{
		{Rating: contentsafety.General, ExplicitProbability: math.NaN()},
		{Rating: contentsafety.General, ExplicitProbability: math.Inf(1)},
		{Rating: contentsafety.General, Confidence: math.NaN()},
		{Rating: contentsafety.General, Confidence: math.Inf(-1)},
		{Rating: contentsafety.Unknown, Confidence: 1},
		{Rating: contentsafety.Rating(99), Confidence: 1},
	}
	for _, evidence := range invalid {
		if got := documentSafetyEvidence(evidence); got != (documentstore.ContentSafetyEvidence{}) {
			t.Fatalf("invalid evidence = %#v", got)
		}
	}
}
