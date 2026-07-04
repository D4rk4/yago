package pageindex

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedTextKeepsShortTextUnchanged(t *testing.T) {
	text := "a short body"
	if got := boundedText(text); got != text {
		t.Fatalf("boundedText(short) = %q, want unchanged", got)
	}
}

func TestBoundedTextTruncatesOversizedText(t *testing.T) {
	text := strings.Repeat("a", maxExtractedTextBytes+512)
	got := boundedText(text)
	if len(got) > maxExtractedTextBytes {
		t.Fatalf("bounded length = %d, want <= %d", len(got), maxExtractedTextBytes)
	}
	if !strings.HasPrefix(text, got) {
		t.Fatal("bounded text is not a prefix of the original")
	}
}

func TestBoundedTextTruncatesOnRuneBoundary(t *testing.T) {
	// A 3-byte rune straddles the byte bound; truncation must not split it.
	text := strings.Repeat("a", maxExtractedTextBytes-1) + "€"
	got := boundedText(text)
	if !utf8.ValidString(got) {
		t.Fatal("bounded text is not valid UTF-8 (split a rune)")
	}
	if len(got) != maxExtractedTextBytes-1 {
		t.Fatalf("bounded length = %d, want %d (dropped partial rune)",
			len(got), maxExtractedTextBytes-1)
	}
}
