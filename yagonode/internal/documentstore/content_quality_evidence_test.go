package documentstore

import (
	"reflect"
	"testing"
)

func TestContentQualityEvidenceRoundTripsThroughDocumentCodec(t *testing.T) {
	want := ContentQualityEvidence{
		Known:                true,
		Score:                0.25,
		FunctionWordFraction: 0.2,
		SymbolFraction:       0.01,
		AlphabeticFraction:   0.9,
		UniqueTokenFraction:  0.7,
		SpamRisk:             0.375,
	}
	raw, err := (documentCodec{}).Encode(Document{
		NormalizedURL:  "https://example.org/",
		ContentQuality: want,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	doc, err := (documentCodec{}).Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(doc.ContentQuality, want) {
		t.Fatalf("ContentQuality = %#v, want %#v", doc.ContentQuality, want)
	}
}

func TestLegacyDocumentHasUnknownNeutralContentQuality(t *testing.T) {
	doc, err := (documentCodec{}).Decode([]byte(`{"NormalizedURL":"https://example.org/"}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(doc.ContentQuality, ContentQualityEvidence{}) {
		t.Fatalf("ContentQuality = %#v, want zero evidence", doc.ContentQuality)
	}
}
