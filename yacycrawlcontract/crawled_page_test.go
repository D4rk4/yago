package yacycrawlcontract

import (
	"reflect"
	"testing"
	"time"
)

func TestCrawledPageRoundTrip(t *testing.T) {
	text := CrawledPage{
		CanonicalURL: "https://example.org/a",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
		Language:     "en",
	}

	data, err := MarshalCrawledPage(text)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalCrawledPage(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(text, got) {
		t.Errorf("round-trip mismatch:\nwant %#v\ngot  %#v", text, got)
	}
}

func TestUnmarshalCrawledPageRejectsInvalidJSON(t *testing.T) {
	if _, err := UnmarshalCrawledPage([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
