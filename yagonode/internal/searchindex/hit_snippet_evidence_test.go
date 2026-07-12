package searchindex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestResultSnippetUsesBodyTermVectorSurface(t *testing.T) {
	body := strings.Repeat("Вводный текст без совпадения. ", 30) +
		"История о псилобатах и канатоходцах."
	start := strings.Index(body, "псилобатах")
	hit := snippetHit(
		"body",
		"псилобат",
		storedLocationCoordinate(start),
		storedLocationCoordinate(start+len("псилобатах")),
	)
	got := resultSnippet(hit, documentstore.Document{ExtractedText: body}, SearchRequest{
		Query: "псилобаты", Terms: []string{"псилобаты"},
	})
	if !strings.Contains(got, "псилобатах") {
		t.Fatalf("body evidence missing: %q", got)
	}
}

func TestResultSnippetUsesTheMatchedByteOffset(t *testing.T) {
	prefix := "needle appears before unrelated material. " +
		strings.Repeat("filler without the target context. ", 40)
	body := prefix + "The matched needle carries late context."
	start := strings.LastIndex(body, "needle")
	hit := snippetHit(
		"body",
		"needle",
		storedLocationCoordinate(start),
		storedLocationCoordinate(start+len("needle")),
	)
	got := resultSnippet(hit, documentstore.Document{ExtractedText: body}, SearchRequest{
		Query: "needle", Terms: []string{"needle"},
	})
	if !strings.Contains(got, "late context") || strings.Contains(got, "appears before") {
		t.Fatalf("snippet did not use matched offset: %q", got)
	}
}

func TestLocationBiasedSnippetKeepsAWholeShortBody(t *testing.T) {
	body := strings.Repeat("short context ", 12) + "needle"
	start := strings.LastIndex(body, "needle")
	evidence, found := locationSnippetEvidence(
		[]string{body},
		&search.Location{
			Start: storedLocationCoordinate(start),
			End:   storedLocationCoordinate(start + len("needle")),
		},
	)
	if !found {
		t.Fatal("valid location rejected")
	}
	if got := locationBiasedSnippet(evidence, "fallback"); got != body {
		t.Fatalf("short snippet = %q", got)
	}
}

func TestLocationBiasedSnippetBoundsLargeUnicodeBody(t *testing.T) {
	prefix := strings.Repeat("Длинный текст без цели. ", 100_000)
	body := prefix + "Точное совпадение псилобатах находится здесь."
	start := strings.LastIndex(body, "псилобатах")
	evidence, found := locationSnippetEvidence(
		[]string{body},
		&search.Location{
			Start: storedLocationCoordinate(start),
			End:   storedLocationCoordinate(start + len("псилобатах")),
		},
	)
	if !found {
		t.Fatal("valid Unicode byte offsets rejected")
	}
	got := locationBiasedSnippet(evidence, "fallback")
	if !strings.Contains(got, "псилобатах") || len([]rune(got)) > snippetRuneCap+2 {
		t.Fatalf("bounded Unicode snippet = %q", got)
	}
}

func TestResultSnippetUsesHeadingAndAnchorEvidence(t *testing.T) {
	doc := documentstore.Document{
		Title:         "Page",
		Headings:      []string{"Introduction", "Rare psilobate research"},
		ExtractedText: strings.Repeat("unrelated body text ", 30),
		Inlinks:       []documentstore.AnchorText{{Text: "Trusted psilobate reference"}},
	}
	headingHit := snippetArrayHit("headings", "psilobate", 1, 5, 14)
	if got := resultSnippet(headingHit, doc, SearchRequest{
		Query: "psilobate", Terms: []string{"psilobate"},
	}); got != doc.Headings[1] {
		t.Fatalf("heading snippet = %q", got)
	}
	doc.Headings = nil
	anchorHit := snippetArrayHit("anchors", "psilobate", 0, 8, 17)
	if got := resultSnippet(anchorHit, doc, SearchRequest{
		Query: "psilobate", Terms: []string{"psilobate"},
	}); got != doc.Inlinks[0].Text {
		t.Fatalf("anchor snippet = %q", got)
	}
}

func TestSearchRendersHeadingAndAnchorOnlyMatches(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://example.org/heading",
				Title:         "Heading page",
				Headings:      []string{"Rare psilobate research"},
				ExtractedText: strings.Repeat("unrelated body text ", 30),
				Language:      "en",
			},
			{
				NormalizedURL: "https://example.org/anchor",
				Title:         "Anchor page",
				ExtractedText: strings.Repeat("unrelated body text ", 30),
				Inlinks: []documentstore.AnchorText{{
					Text: "Trusted xylophone reference",
				}},
				Language: "en",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	heading, err := index.Search(t.Context(), SearchRequest{
		Query: "psilobate", Terms: []string{"psilobate"}, MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("heading Search: %v", err)
	}
	if len(heading.Results) != 1 ||
		heading.Results[0].Snippet != "Rare psilobate research" {
		t.Fatalf("heading results = %#v", heading.Results)
	}
	anchor, err := index.Search(t.Context(), SearchRequest{
		Query: "xylophone", Terms: []string{"xylophone"}, MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("anchor Search: %v", err)
	}
	if len(anchor.Results) != 1 ||
		anchor.Results[0].Snippet != "Trusted xylophone reference" {
		t.Fatalf("anchor results = %#v", anchor.Results)
	}
}

func TestSearchRendersMorphologyEvidencePastLargeBodyBoundary(t *testing.T) {
	body := strings.Repeat("Вводный материал без совпадения. ", 3_000) +
		"История о псилобатах и канатоходцах."
	if len(body) <= 64<<10 {
		t.Fatalf("fixture body is only %d bytes", len(body))
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.org/large-morphology",
			Title:         "Старинное слово",
			ExtractedText: body,
			Language:      "ru",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "псилобаты", Terms: []string{"псилобаты"}, MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 ||
		!strings.Contains(result.Results[0].Snippet, "псилобатах") {
		t.Fatalf("large morphology results = %#v", result.Results)
	}
}

func TestSearchUsesIndexedAnalyzerWhenStoredLanguageIsWrong(t *testing.T) {
	body := strings.Repeat(
		"Русская литература объединяет поэзию, прозу и драматургию. "+
			"Писатели описывают исторические события, жизнь людей и общественные перемены. ",
		100,
	) +
		"История о псилобатах и канатоходцах."
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/wrong-language",
		Title:         "Старинное слово",
		ExtractedText: body,
		Language:      "en",
	}
	indexed, err := bleveDocumentFromStore(doc)
	if err != nil {
		t.Fatal(err)
	}
	if analyzer := indexed.Analyzer; analyzer != "ru" {
		t.Fatalf("indexed analyzer = %q", analyzer)
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{doc},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "псилобаты", Terms: []string{"псилобаты"}, MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 ||
		!strings.Contains(result.Results[0].Snippet, "псилобатах") {
		t.Fatalf("wrong-language results = %#v", result.Results)
	}
}

func TestStoredEvidenceRecoversMissingAnalyzerMarker(t *testing.T) {
	body := strings.Repeat(
		"Русские писатели создают романы, поэмы и рассказы о жизни людей. ",
		200,
	) + "История о псилобатах и канатоходцах."
	doc := documentstore.Document{
		Title:         "Старинное слово",
		ExtractedText: body,
		Language:      "en",
	}
	indexed, err := bleveDocumentFromStore(doc)
	if err != nil {
		t.Fatal(err)
	}
	if analyzer := indexed.Analyzer; analyzer != "ru" {
		t.Fatalf("indexed analyzer = %q", analyzer)
	}
	result, err := searchResultFromStoredEvidence(
		t.Context(),
		&search.DocumentMatch{},
		doc,
		SearchRequest{Query: "псилобаты", Terms: []string{"псилобаты"}},
	)
	if err != nil {
		t.Fatalf("searchResultFromStoredEvidence: %v", err)
	}
	if !strings.Contains(result.Snippet, "псилобатах") {
		t.Fatalf("missing-marker snippet = %q", result.Snippet)
	}
}

func TestSearchRendersFuzzyTranspositionEvidence(t *testing.T) {
	body := strings.Repeat("unrelated filler text. ", 1_000) +
		"The golang evidence is near the end."
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.org/fuzzy-transposition",
			Title:         "Programming language",
			ExtractedText: body,
			Language:      "en",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "golnag", Terms: []string{"golnag"}, MaxResults: 5, Fuzzy: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 ||
		!strings.Contains(result.Results[0].Snippet, "golang") {
		t.Fatalf("fuzzy transposition results = %#v", result.Results)
	}
}

func TestStoredEvidencePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := searchResultFromStoredEvidence(
		ctx,
		&search.DocumentMatch{},
		documentstore.Document{ExtractedText: "needle evidence"},
		SearchRequest{Query: "needle", Terms: []string{"needle"}},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestLocationSnippetEvidenceRejectsInvalidOffsets(t *testing.T) {
	invalid := []*search.Location{
		nil,
		{Start: 3, End: 3},
		{Start: 0, End: 100},
		{Start: 0, End: 1, ArrayPositions: search.ArrayPositions{2}},
	}
	for _, location := range invalid {
		if _, found := locationSnippetEvidence([]string{"text"}, location); found {
			t.Fatalf("invalid location admitted: %#v", location)
		}
	}
	if _, found := locationSnippetEvidence(nil, &search.Location{Start: 0, End: 1}); found {
		t.Fatal("empty values admitted")
	}
	if _, found := locationSnippetEvidence(
		[]string{"é"},
		&search.Location{Start: 0, End: 1},
	); found {
		t.Fatal("split UTF-8 location admitted")
	}
	if got := locationBiasedSnippet(snippetEvidence{text: " "}, "fallback"); got != "fallback" {
		t.Fatalf("blank location snippet = %q", got)
	}
}

func TestResultSnippetFallsBackFromInvalidLocations(t *testing.T) {
	doc := documentstore.Document{ExtractedText: "fallback body text"}
	hit := snippetHit("body", "needle", 0, 100)
	got := resultSnippet(hit, doc, SearchRequest{
		Query: "needle", Terms: []string{"needle"},
	})
	if got != doc.ExtractedText {
		t.Fatalf("fallback snippet = %q", got)
	}
}

func TestFieldSnippetEvidenceSkipsAnInvalidLeadingLocation(t *testing.T) {
	hit := snippetHit("body", "needle", 5, 11)
	hit.Locations["body"]["needle"] = append(
		search.Locations{nil},
		hit.Locations["body"]["needle"]...,
	)
	evidence, found := fieldSnippetEvidence(
		hit,
		"body",
		[]string{"text needle"},
		map[string]struct{}{"needle": {}},
		false,
	)
	if !found || evidence.start != 5 {
		t.Fatalf("evidence = %#v, found = %t", evidence, found)
	}
}

func TestAnalyzedQueryTermsHandlesBlankAndUnavailableAnalyzers(t *testing.T) {
	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	terms := analyzedQueryTerms(SearchRequest{Terms: []string{" ", "Needle"}})
	if len(terms) != 1 {
		t.Fatalf("terms = %#v", terms)
	}

	loadStemmingMapping = mapping.NewIndexMapping
	terms = analyzedQueryTerms(SearchRequest{Query: "needle", Terms: []string{"needle"}})
	if _, found := terms["needle"]; !found {
		t.Fatalf("terms = %#v", terms)
	}
}

func TestFieldSnippetEvidenceRejectsUnrelatedTerms(t *testing.T) {
	hit := snippetHit("body", "unrelated", 0, 9)
	if evidence, found := fieldSnippetEvidence(
		hit,
		"body",
		[]string{"unrelated"},
		map[string]struct{}{"needle": {}},
		false,
	); found {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func snippetHit(field string, term string, start uint64, end uint64) *search.DocumentMatch {
	return &search.DocumentMatch{Locations: search.FieldTermLocationMap{
		field: search.TermLocationMap{
			term: search.Locations{{Start: start, End: end}},
		},
	}}
}

func snippetArrayHit(
	field string,
	term string,
	arrayIndex uint64,
	start uint64,
	end uint64,
) *search.DocumentMatch {
	hit := snippetHit(field, term, start, end)
	hit.Locations[field][term][0].ArrayPositions = search.ArrayPositions{arrayIndex}

	return hit
}
