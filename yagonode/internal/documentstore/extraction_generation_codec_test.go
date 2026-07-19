package documentstore

import (
	"bytes"
	"strconv"
	"testing"
)

func TestDocumentCodecExtractionGenerationAndLegacyCompatibility(t *testing.T) {
	t.Parallel()
	const extractionGeneration = uint64(37)

	raw, err := (documentCodec{}).Encode(Document{
		NormalizedURL:        "https://example.test/current",
		ExtractionGeneration: extractionGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	generationField := []byte(
		`"ExtractionGeneration":` + strconv.FormatUint(extractionGeneration, 10),
	)
	if !bytes.Contains(raw, generationField) {
		t.Fatalf("encoded generation = %s", raw)
	}
	current, err := (documentCodec{}).Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if current.ExtractionGeneration != extractionGeneration {
		t.Fatalf("generation = %d", current.ExtractionGeneration)
	}

	legacy, err := (documentCodec{}).Decode([]byte(
		`{"NormalizedURL":"https://example.test/legacy"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ExtractionGeneration != 0 {
		t.Fatalf("legacy generation = %d, want 0", legacy.ExtractionGeneration)
	}
	zero, err := (documentCodec{}).Encode(Document{
		NormalizedURL: "https://example.test/legacy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(zero, []byte("ExtractionGeneration")) {
		t.Fatalf("zero generation was not omitted: %s", zero)
	}
}
