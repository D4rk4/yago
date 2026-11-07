package searchindex

import (
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestQuerySnippetPrefersCompleteWitness(t *testing.T) {
	body := "Needle alone. " + strings.Repeat("unrelated background. ", 80) +
		"Needle compass appear together."
	evidence, _, available, err := AnalyzeDocumentQueryEvidence(
		t.Context(),
		documentstore.Document{Title: "Report", ExtractedText: body, Language: "en"},
		[]string{"needle", "compass"},
		MaximumDocumentQueryEvidenceBytes,
	)
	if err != nil || !available {
		t.Fatalf("available=%v err=%v", available, err)
	}
	if !strings.Contains(evidence.Snippet, "Needle compass appear together.") ||
		len(evidence.SnippetMatches) != 2 {
		t.Fatalf("snippet=%q matches=%#v", evidence.Snippet, evidence.SnippetMatches)
	}
}

func TestSearchSnippetPrefersCompleteWitness(t *testing.T) {
	body := "Needle alone. " + strings.Repeat("unrelated background. ", 80) +
		"Needle compass appear together."
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.org/report",
			Title:         "Report",
			ExtractedText: body,
			Language:      "en",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	found, err := index.Search(t.Context(), SearchRequest{
		Query: "needle compass", Terms: []string{"needle", "compass"}, MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found.Results) != 1 ||
		!strings.Contains(found.Results[0].Snippet, "Needle compass appear together.") {
		t.Fatalf("results = %#v", found.Results)
	}
}

func TestBothSnippetPathsAlignToSentences(t *testing.T) {
	body := strings.Repeat("filler words ", 30) + "Prior remark ends. " +
		"Needle compass appear together. " + strings.Repeat("closing words ", 30)
	want := "… Needle compass appear together."
	if got := documentEvidenceSnippet(t, body, "en", []string{"needle", "compass"}); got != want {
		t.Fatalf("document evidence snippet = %q", got)
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.org/report",
			Title:         "Report",
			ExtractedText: body,
			Language:      "en",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	found, err := index.Search(t.Context(), SearchRequest{
		Query: "needle compass", Terms: []string{"needle", "compass"}, MaxResults: 5,
	})
	if err != nil || len(found.Results) != 1 || found.Results[0].Snippet != want {
		t.Fatalf("search results = %#v err = %v", found.Results, err)
	}
}

func TestQuerySnippetMatchesMorphologyWithTheDocumentAnalyzer(t *testing.T) {
	for _, test := range []struct {
		language     string
		requirements []string
		body         string
		want         string
	}{
		{
			language:     "en",
			requirements: []string{"needle", "compass"},
			body: "Needles alone. " + strings.Repeat("unrelated background. ", 80) +
				"Compasses and needles appear together.",
			want: "Compasses and needles appear together.",
		},
		{
			language:     "ru",
			requirements: []string{"игла", "компас"},
			body: "Иглы отдельно. " + strings.Repeat("посторонний фон. ", 80) +
				"Компасы и иглы лежат вместе.",
			want: "Компасы и иглы лежат вместе.",
		},
	} {
		got := documentEvidenceSnippet(t, test.body, test.language, test.requirements)
		if !strings.Contains(got, test.want) {
			t.Fatalf("%s snippet = %q", test.language, got)
		}
	}
}

func TestQuerySnippetIgnoresSubstringCollisions(t *testing.T) {
	body := "Needle encompassing ideas. " + strings.Repeat("unrelated background. ", 80) +
		"Needle compass appear together."
	got := documentEvidenceSnippet(t, body, "en", []string{"needle", "compass"})
	if !strings.Contains(got, "Needle compass appear together.") {
		t.Fatalf("snippet = %q", got)
	}
}

func TestQuotedPhraseSnippetKeepsPositionOrderAndRanking(t *testing.T) {
	background := strings.Repeat("unrelated background. ", 80)
	documents := []documentstore.Document{
		{ExtractedText: "Compass beside a needle. " + background, Language: "en"},
		{
			ExtractedText: "Needle alone. " + background + "Needle compass appear together.",
			Language:      "en",
		},
	}
	results, err := searchEvidenceResults(
		t.Context(),
		SearchRequest{
			Query: `"needle compass"`, Terms: []string{"needle", "compass"},
			Phrases: []string{"needle compass"},
		},
		[]SearchResult{
			{DocumentID: "scattered", Score: 1, Analyzer: searchTextAnalyzer},
			{DocumentID: "adjacent", Score: 1, Analyzer: searchTextAnalyzer},
		},
		func(index int) (documentstore.Document, bool, error) {
			return documents[index], true, nil
		},
	)
	if err != nil {
		t.Fatalf("searchEvidenceResults: %v", err)
	}
	if len(results) != 2 || results[0].DocumentID != "adjacent" ||
		!strings.Contains(results[0].Snippet, "Needle compass appear together.") ||
		!strings.HasPrefix(results[1].Snippet, "Compass beside a needle.") {
		t.Fatalf("results = %#v", results)
	}
}

func TestFieldSnippetEvidenceKeepsTheEarliestSingleWitness(t *testing.T) {
	body := "needle first. needle second."
	witness, found := fieldSnippetEvidence(
		search.TermLocationMap{"needle": {
			byteLocation(14, "needle"),
			byteLocation(0, "needle"),
		}},
		[]string{body},
		nil,
		true,
	)
	if !found || witness.coverage != 1 ||
		witness.evidence != (TextQueryEvidence{Start: 0, End: 6}) {
		t.Fatalf("witness = %#v", witness)
	}
}

func TestFieldSnippetEvidencePrefersTheSmallerSpread(t *testing.T) {
	body := "needle " + strings.Repeat("x ", 40) + "compass. needle compass."
	wideCompass := strings.Index(body, "compass")
	tightNeedle := strings.LastIndex(body, "needle")
	witness, found := fieldSnippetEvidence(
		search.TermLocationMap{
			"needle": {byteLocation(0, "needle"), byteLocation(tightNeedle, "needle")},
			"compass": {
				byteLocation(wideCompass, "compass"),
				byteLocation(tightNeedle+7, "compass"),
			},
		},
		[]string{body},
		nil,
		true,
	)
	want := TextQueryEvidence{Start: tightNeedle, End: tightNeedle + len("needle compass")}
	if !found || witness.coverage != 2 || witness.evidence != want {
		t.Fatalf("witness = %#v, want %#v", witness, want)
	}
}

func TestFieldSnippetEvidenceRefusesToJoinOccurrencesBeyondTheCap(t *testing.T) {
	for _, test := range []struct {
		gap          int
		wantCoverage int
	}{
		{gap: snippetRuneCap - len("needle ") - len("compass"), wantCoverage: 2},
		{gap: snippetRuneCap - len("needle ") - len("compass") + 1, wantCoverage: 1},
	} {
		body := "needle " + strings.Repeat("ж", test.gap) + "compass"
		compass := strings.LastIndex(body, "compass")
		witness, found := fieldSnippetEvidence(
			search.TermLocationMap{
				"needle":  {byteLocation(0, "needle")},
				"compass": {byteLocation(compass, "compass")},
			},
			[]string{body},
			nil,
			true,
		)
		if !found || witness.coverage != test.wantCoverage || witness.evidence.Start != 0 {
			t.Fatalf("gap %d witness = %#v", test.gap, witness)
		}
	}
}

func TestFieldSnippetEvidenceDropsARepeatedLeadingRequirement(t *testing.T) {
	body := "needle needle compass"
	witness, found := fieldSnippetEvidence(
		search.TermLocationMap{
			"needle":  {byteLocation(0, "needle"), byteLocation(7, "needle")},
			"compass": {byteLocation(14, "compass")},
		},
		[]string{body},
		nil,
		true,
	)
	if !found || witness.evidence != (TextQueryEvidence{Start: 7, End: len(body)}) {
		t.Fatalf("witness = %#v", witness)
	}
}

func TestFieldSnippetEvidenceKeepsEachValueSeparate(t *testing.T) {
	headings := []string{"needle only", "compass only", "needle and compass"}
	terms := search.TermLocationMap{
		"needle": {
			arrayLocation(headings, 0, "needle"),
			arrayLocation(headings, 2, "needle"),
		},
		"compass": {
			arrayLocation(headings, 1, "compass"),
			arrayLocation(headings, 2, "compass"),
		},
	}
	witness, found := fieldSnippetEvidence(terms, headings, nil, true)
	if !found || witness.arrayIndex != 2 || witness.coverage != 2 {
		t.Fatalf("witness = %#v", witness)
	}
	witness, found = fieldSnippetEvidence(terms, headings[:2], nil, true)
	if !found || witness.arrayIndex != 0 || witness.coverage != 1 {
		t.Fatalf("separate values joined: %#v", witness)
	}
}

func TestFieldSnippetEvidenceAdmitsAnyTermOnlyWhenAsked(t *testing.T) {
	terms := search.TermLocationMap{"neddle": {{Start: 0, End: 6}}}
	if _, found := fieldSnippetEvidence(
		terms,
		[]string{"neddle"},
		map[string]struct{}{"needle": {}},
		false,
	); found {
		t.Fatal("disallowed term admitted")
	}
	if _, found := fieldSnippetEvidence(
		terms,
		[]string{"neddle"},
		map[string]struct{}{"needle": {}},
		true,
	); !found {
		t.Fatal("fuzzy term refused")
	}
}

func TestSentenceSnippetBoundsAnOverCapSingleWitness(t *testing.T) {
	body := "Lead. " + strings.Repeat("z", snippetRuneCap+40) + " tail."
	witness, found := fieldSnippetEvidence(
		search.TermLocationMap{"z": {{
			Start: storedLocationCoordinate(len("Lead. ")),
			End:   storedLocationCoordinate(len(body) - len(" tail.")),
		}}},
		[]string{body},
		nil,
		true,
	)
	if !found {
		t.Fatal("over-cap witness refused")
	}
	if got := sentenceSnippet(
		witness,
		"fallback",
	); got != boundedSnippet(
		body,
		witness.evidence,
		"fallback",
	) {
		t.Fatalf("over-cap snippet = %q", got)
	}
}

func byteLocation(start int, surface string) *search.Location {
	return &search.Location{
		Start: storedLocationCoordinate(start),
		End:   storedLocationCoordinate(start + len(surface)),
	}
}

func arrayLocation(values []string, arrayIndex int, surface string) *search.Location {
	start := strings.Index(values[arrayIndex], surface)

	return &search.Location{
		Start:          storedLocationCoordinate(start),
		End:            storedLocationCoordinate(start + len(surface)),
		ArrayPositions: search.ArrayPositions{storedLocationCoordinate(arrayIndex)},
	}
}
