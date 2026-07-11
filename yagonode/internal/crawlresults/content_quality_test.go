package crawlresults

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestDocumentFromIngestComputesContentQualityEvidence(t *testing.T) {
	text := "the cat and dog are in the house and sun bright alpha beta gamma delta epsilon zeta eta theta iota"
	doc := documentFromIngest(yagocrawlcontract.DocumentIngest{
		NormalizedURL: "https://example.org/",
		ExtractedText: text,
	})
	want := documentstore.ContentQualityEvidence{
		Known:                true,
		Score:                1,
		FunctionWordFraction: 0.3,
		SymbolFraction:       0,
		AlphabeticFraction:   1,
		UniqueTokenFraction:  0.9,
		SpamRisk:             0,
	}
	if !reflect.DeepEqual(doc.ContentQuality, want) {
		t.Fatalf("ContentQuality = %#v, want %#v", doc.ContentQuality, want)
	}

	doc.ExtractedText = "changed after ingest"
	if !reflect.DeepEqual(doc.ContentQuality, want) {
		t.Fatalf("ContentQuality changed with text field: %#v", doc.ContentQuality)
	}
}

func TestContentQualityFromTextKeepsUnknownEvidenceNeutral(t *testing.T) {
	got := contentQualityFromText("short text")
	if !reflect.DeepEqual(got, documentstore.ContentQualityEvidence{}) {
		t.Fatalf("ContentQuality = %#v, want zero evidence", got)
	}
}
