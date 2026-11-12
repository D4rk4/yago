package documentstore

import (
	"slices"
	"testing"
)

func TestCanonicalAnchorTextsSortsFieldsAndRemovesExactDuplicates(t *testing.T) {
	anchors := []AnchorText{
		{URL: "https://b.example/", Text: "second source"},
		{URL: "https://a.example/", Text: "z"},
		{URL: "https://a.example/", Text: "a"},
		{URL: "https://a.example/", Text: "a"},
		{URL: "https://a.example/", Sponsored: true},
		{URL: "https://a.example/", NoFollow: true},
	}
	want := []AnchorText{
		{URL: "https://a.example/", Text: "a"},
		{URL: "https://a.example/", Text: "z"},
		{URL: "https://a.example/", Sponsored: true},
		{URL: "https://a.example/", NoFollow: true},
		{URL: "https://b.example/", Text: "second source"},
	}
	if got := canonicalAnchorTexts(anchors); !slices.Equal(got, want) {
		t.Fatalf("canonical anchors = %#v, want %#v", got, want)
	}
}
