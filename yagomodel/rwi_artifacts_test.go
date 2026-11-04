package yagomodel_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestBuildRWIPageArtifactsPreservesSourceDatesAndPositions(t *testing.T) {
	loadedAt := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	modifiedAt := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	freshUntil := time.Date(2024, 4, 5, 0, 0, 0, 0, time.UTC)
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:               "https://example.test/path",
		Title:             "Example, title",
		Language:          "fr-CA",
		WordCount:         70000,
		TitleWordCount:    2,
		TextBytes:         27,
		LocalLinkCount:    300,
		ExternalLinkCount: 4,
		Words: []yagomodel.RWIWord{
			{Word: "sample", Frequency: 3, FirstPosition: 1},
			{Word: "title", Frequency: 1, FirstPosition: 4},
		},
		LoadedAt:   loadedAt,
		ModifiedAt: modifiedAt,
		FreshUntil: freshUntil,
	}, 8)
	if len(artifacts.Postings) != 2 {
		t.Fatalf("posting rows = %d, want 2", len(artifacts.Postings))
	}
	if got := artifacts.Postings[0].Properties[yagomodel.ColLanguage]; got != "fr" {
		t.Errorf("posting language = %q, want fr", got)
	}
	if got := artifacts.Postings[0].Properties[yagomodel.ColTextPosition]; got == "" {
		t.Error("posting position is empty")
	}
	for key, want := range map[string]string{
		yagomodel.ColTextWordCount:     yagomodel.FormatRWICardinal(0xffff),
		yagomodel.ColLocalLinkCount:    yagomodel.FormatRWICardinal(0xff),
		yagomodel.ColExternalLinkCount: yagomodel.FormatRWICardinal(4),
	} {
		if got := artifacts.Postings[0].Properties[key]; got != want {
			t.Errorf("posting cardinal %s = %q, want %q", key, got, want)
		}
	}
	metadata, err := yagomodel.ParseURIMetadataRow(artifacts.Metadata.String())
	if err != nil {
		t.Fatalf("ParseURIMetadataRow: %v", err)
	}
	for key, want := range map[string]string{
		"mod":    "20230201",
		"load":   "20240304",
		"fresh":  "20240405",
		"size":   "27",
		"wc":     "70000",
		"llocal": "300",
		"lother": "4",
		"lang":   "fr",
	} {
		if got := metadata.Properties[key]; got != want {
			t.Errorf("metadata %s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildRWIPageArtifactsClampsNegativeCardinalsToZero(t *testing.T) {
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:               "https://example.test/negative",
		WordCount:         -1,
		LocalLinkCount:    -1,
		ExternalLinkCount: -1,
		Words: []yagomodel.RWIWord{
			{Word: "negative", Frequency: -1, FirstPosition: -1},
		},
	}, 1)
	posting := artifacts.Postings[0]
	for key, want := range map[string]string{
		yagomodel.ColTextWordCount:     yagomodel.FormatRWICardinal(0),
		yagomodel.ColLocalLinkCount:    yagomodel.FormatRWICardinal(0),
		yagomodel.ColExternalLinkCount: yagomodel.FormatRWICardinal(0),
		yagomodel.ColHitCount:          yagomodel.FormatRWICardinal(0),
		yagomodel.ColTextPosition:      yagomodel.FormatRWICardinal(0),
	} {
		if got := posting.Properties[key]; got != want {
			t.Errorf("posting cardinal %s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildRWIPageArtifactsPreservesLegacyURLAndZeroDateBehavior(t *testing.T) {
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:   "relative/path",
		Words: []yagomodel.RWIWord{{Word: "legacy", Frequency: 1}},
	}, 1)
	if len(artifacts.Postings) != 1 {
		t.Fatalf("postings = %d, want 1", len(artifacts.Postings))
	}
	metadata := artifacts.Metadata.Properties
	for _, key := range []string{"mod", "load", "fresh"} {
		if metadata[key] != "00010101" {
			t.Errorf("metadata %s = %q, want 00010101", key, metadata[key])
		}
	}
}

func TestBuildRWIPageArtifactsRetainsMalformedLegacyURL(t *testing.T) {
	const rawURL = "http://[::1"
	wantHash, err := yagomodel.HashURL(rawURL)
	if err != nil {
		t.Fatalf("HashURL(%q): %v", rawURL, err)
	}
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:   rawURL,
		Words: []yagomodel.RWIWord{{Word: "legacy", Frequency: 1}},
	}, 1)
	if len(artifacts.Postings) != 1 {
		t.Fatalf("postings = %d, want one legacy posting", len(artifacts.Postings))
	}
	if got := artifacts.Postings[0].Properties[yagomodel.ColURLHash]; got != wantHash.String() {
		t.Errorf("posting URL hash = %q, want %q", got, wantHash)
	}
	if got := artifacts.Metadata.Properties[yagomodel.URLMetaHash]; got != wantHash.String() {
		t.Errorf("metadata URL hash = %q, want %q", got, wantHash)
	}
}

func TestBuildRWIPageArtifactsTreatsNonpositivePostingLimitAsEmpty(t *testing.T) {
	page := yagomodel.RWIPage{
		URL:   "https://example.test/",
		Words: []yagomodel.RWIWord{{Word: "sample", Frequency: 1}},
	}
	for _, maximum := range []int{0, -1} {
		if artifacts := yagomodel.BuildRWIPageArtifacts(
			page,
			maximum,
		); len(
			artifacts.Postings,
		) != 0 {
			t.Errorf("maximum %d produced %d postings", maximum, len(artifacts.Postings))
		}
	}
}

func TestTokenizeRWITextUsesYaCyWordBoundaries(t *testing.T) {
	for _, test := range []struct {
		text string
		want []string
	}{
		{text: "alpha beta 12.5 c++ co-op", want: []string{"alpha", "beta", "12.5", "co-op"}},
		{text: "Montréal Québec", want: []string{"montréal", "québec"}},
		{text: "alpha beta gamma", want: []string{"alpha", "beta"}},
	} {
		maximum := 0
		if test.text == "alpha beta gamma" {
			maximum = 2
		}
		got := yagomodel.TokenizeRWIText(test.text, maximum)
		if len(got) != len(test.want) {
			t.Fatalf("TokenizeRWIText(%q) = %q, want %q", test.text, got, test.want)
		}
		for index := range got {
			if got[index] != test.want[index] {
				t.Fatalf("TokenizeRWIText(%q) = %q, want %q", test.text, got, test.want)
			}
		}
	}
}

func TestTokenizeRWITextStopsAtTheRequestedWordBoundary(t *testing.T) {
	for _, test := range []struct {
		text    string
		maximum int
		want    []string
	}{
		{text: "alpha beta", maximum: 1, want: []string{"alpha"}},
		{text: "alpha.beta", maximum: 1, want: []string{"alpha"}},
		{text: "1,2.", maximum: 1, want: []string{"1,2."}},
		{text: "1,2!", maximum: 0, want: []string{"1,2!"}},
		{text: "12a", maximum: 1, want: []string{"12"}},
		{text: "1,2a", maximum: 0, want: []string{"1,2"}},
	} {
		if got := yagomodel.TokenizeRWIText(
			test.text,
			test.maximum,
		); !reflect.DeepEqual(
			got,
			test.want,
		) {
			t.Errorf(
				"TokenizeRWIText(%q, %d) = %q, want %q",
				test.text,
				test.maximum,
				got,
				test.want,
			)
		}
	}
}
