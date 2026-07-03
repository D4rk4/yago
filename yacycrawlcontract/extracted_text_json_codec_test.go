package yacycrawlcontract

import (
	"reflect"
	"testing"
	"time"
)

func TestExtractedTextRoundTrip(t *testing.T) {
	text := ExtractedText{
		CanonicalURL: "https://example.org/a",
		DocumentID:   "abc123",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
		Language:     "en",
	}

	data, err := MarshalExtractedText(text)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalExtractedText(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(text, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", text, got)
	}
}

func TestUnmarshalExtractedTextRejectsInvalidJSON(t *testing.T) {
	if _, err := UnmarshalExtractedText([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
