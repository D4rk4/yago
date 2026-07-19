package yagocrawlcontract

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestIngestBatchExtractionGenerationRoundTripAndLegacyCompatibility(t *testing.T) {
	t.Parallel()

	want := IngestBatch{Document: DocumentIngest{
		NormalizedURL:        "https://example.test/current",
		ExtractionGeneration: CurrentExtractionGeneration,
	}}
	raw, err := MarshalIngestBatch(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"ExtractionGeneration":1`)) {
		t.Fatalf("encoded generation = %s", raw)
	}
	got, err := UnmarshalIngestBatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Document.ExtractionGeneration != CurrentExtractionGeneration {
		t.Fatalf("generation = %d", got.Document.ExtractionGeneration)
	}
	var olderDecoder struct {
		Document struct {
			NormalizedURL string
		}
	}
	if err := json.Unmarshal(raw, &olderDecoder); err != nil {
		t.Fatal(err)
	}
	if olderDecoder.Document.NormalizedURL != want.Document.NormalizedURL {
		t.Fatalf("older decoder document = %+v", olderDecoder.Document)
	}

	legacy, err := UnmarshalIngestBatch([]byte(
		`{"Document":{"NormalizedURL":"https://example.test/legacy"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Document.ExtractionGeneration != 0 {
		t.Fatalf("legacy generation = %d, want 0", legacy.Document.ExtractionGeneration)
	}
	zero, err := MarshalIngestBatch(IngestBatch{Document: DocumentIngest{
		NormalizedURL: "https://example.test/legacy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(zero, []byte("ExtractionGeneration")) {
		t.Fatalf("zero generation was not omitted: %s", zero)
	}
}
