package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func newRequiredTermsFixture(t *testing.T) *BleveMemoryIndex {
	t.Helper()
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://example.org/mn-beaches",
				Title:         "Черногория",
				ExtractedText: "Черногория и её пляжи на побережье Адриатики.",
				Language:      "ru",
			},
			{
				NormalizedURL: "https://example.org/es-beaches",
				Title:         "Пляжи",
				ExtractedText: "Пляжи Испании считаются одними из лучших в Европе.",
				Language:      "ru",
			},
			{
				NormalizedURL: "https://anticisco.ru/forum",
				Title:         "Интернет-ресурсы",
				ExtractedText: "Интернет ресурсы по маршрутизаторам и сетям, форум провайдеров.",
				Language:      "ru",
			},
			{
				NormalizedURL: "https://example.org/mn-isp",
				Title:         "Интернет в Черногории",
				ExtractedText: "Черногория и её интернет провайдеры на побережье.",
				Language:      "ru",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	return index
}

// TestSearchRequiresEveryQueryWord is the SEARCH-27 headline: a document
// matching only one of two query words never surfaces, the all-words parity of
// YaCy's RWI join.
func TestSearchRequiresEveryQueryWord(t *testing.T) {
	index := newRequiredTermsFixture(t)
	got, err := index.Search(t.Context(), SearchRequest{
		Query:      "черногория пляжи",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Total != 1 || got.Results[0].URL != "https://example.org/mn-beaches" {
		t.Fatalf("conjunction results = %#v", got.Results)
	}
}

// TestSearchStopwordDoesNotVetoConjunction proves an analyzed-away function
// word («в») cannot demand itself of documents whose analyzer stripped it at
// index time.
func TestSearchStopwordDoesNotVetoConjunction(t *testing.T) {
	index := newRequiredTermsFixture(t)
	got, err := index.Search(t.Context(), SearchRequest{
		Query:      "интернет в черногории",
		Terms:      []string{"интернет", "в", "черногории"},
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Total != 1 || got.Results[0].URL != "https://example.org/mn-isp" {
		t.Fatalf("stopword query results = %#v", got.Results)
	}
}

func TestSearchUsesAnalyzerSpecificStopwordConjunctions(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://example.org/english-cat",
				ExtractedText: "A cat sleeps quietly.",
				Language:      "en",
			},
			{
				NormalizedURL: "https://example.org/english-hard",
				ExtractedText: "A hard problem.",
				Language:      "en",
			},
			{
				NormalizedURL: "https://example.org/english-die-hard",
				ExtractedText: "The Die Hard film.",
				Language:      "en",
			},
			{
				NormalizedURL: "https://example.org/german-cat",
				ExtractedText: "Eine Katze schläft.",
				Language:      "de",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	theCat, err := index.Search(t.Context(), SearchRequest{
		Query: "the cat", Terms: []string{"the", "cat"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search the cat: %v", err)
	}
	if theCat.Total != 1 || theCat.Results[0].URL != "https://example.org/english-cat" {
		t.Fatalf("the cat results = %#v", theCat.Results)
	}
	dieHard, err := index.Search(t.Context(), SearchRequest{
		Query: "die hard", Terms: []string{"die", "hard"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search die hard: %v", err)
	}
	if dieHard.Total != 1 ||
		dieHard.Results[0].URL != "https://example.org/english-die-hard" {
		t.Fatalf("die hard results = %#v", dieHard.Results)
	}
	dieKatze, err := index.Search(t.Context(), SearchRequest{
		Query: "die katze", Terms: []string{"die", "katze"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search die katze: %v", err)
	}
	if dieKatze.Total != 1 || dieKatze.Results[0].URL != "https://example.org/german-cat" {
		t.Fatalf("die katze results = %#v", dieKatze.Results)
	}
}

func TestExcludedTermsUseTheDocumentAnalyzerAcrossFields(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://example.org/hunde",
			Title:         "Hunde",
			Headings:      []string{"Laufen"},
			ExtractedText: "Hunde laufen schnell.",
			Language:      "de",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "hund", Terms: []string{"hund"}, ExcludeTerms: []string{"laufen"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total != 0 || len(result.Results) != 0 {
		t.Fatalf("excluded German stem admitted: %#v", result.Results)
	}
}

func TestDiskSearchFiltersCrossLanguageStopwordBranches(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/hard-only",
			ExtractedText: "A hard problem.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/die-hard",
			ExtractedText: "The Die Hard film.",
			Language:      "en",
		},
	}
	directory := newFakeDocumentDirectory(documents...)
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		directory,
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	got, err := index.Search(t.Context(), SearchRequest{
		Query: "die hard", Terms: []string{"die", "hard"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Total != 1 || len(got.Results) != 1 ||
		got.Results[0].URL != "https://example.org/die-hard" {
		t.Fatalf("disk results = %#v", got.Results)
	}
}

// TestSearchAllStopwordQueryFallsBack keeps an all-function-word query on the
// legacy whole-query clause instead of demanding words no index holds.
func TestSearchAllStopwordQueryFallsBack(t *testing.T) {
	index := newRequiredTermsFixture(t)
	got, err := index.Search(t.Context(), SearchRequest{Query: "в и на", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Total != 0 {
		t.Fatalf("all-stopword query matched %d documents", got.Total)
	}
}

// TestExpansionTermsNeverAdmitDocuments is the RM3 drift-control contract:
// expansion terms reorder documents that already hold every query word and
// cannot surface one that does not.
func TestExpansionTermsNeverAdmitDocuments(t *testing.T) {
	index := newRequiredTermsFixture(t)
	got, err := index.Search(t.Context(), SearchRequest{
		Query:          "черногория",
		ExpansionTerms: []string{"интернет", "провайдер"},
		MaxResults:     10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Total != 2 {
		t.Fatalf("expansion admitted extra documents: %#v", got.Results)
	}
	for _, result := range got.Results {
		if result.URL == "https://anticisco.ru/forum" {
			t.Fatal("expansion-only document leaked into results")
		}
	}
	if got.Results[0].URL != "https://example.org/mn-isp" {
		t.Fatalf("expansion evidence did not lift the richer document: %#v", got.Results)
	}
}

func TestRequirableTermsWithoutMappingKeepsEveryWord(t *testing.T) {
	old := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = old })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }

	terms := requirableTerms([]string{"в", "черногории", " "}, []string{"ru"})
	if len(terms) != 2 {
		t.Fatalf("terms without mapping = %#v", terms)
	}
}

func TestRequirableTermsKeepWordsMeaningfulInAnyCandidateLanguage(t *testing.T) {
	terms := requirableTerms(
		[]string{"die", "hard"},
		[]string{"en", "de", standardTextAnalyzer},
	)
	if len(terms) != 2 || terms[0] != "die" || terms[1] != "hard" {
		t.Fatalf("multilingual terms = %#v", terms)
	}
	if terms := requirableTerms(
		[]string{"в"},
		[]string{"ru", standardTextAnalyzer},
	); len(
		terms,
	) != 0 {
		t.Fatalf("universal stopword terms = %#v", terms)
	}
}

func TestQueryTermWordsPrefersParsedTerms(t *testing.T) {
	req := SearchRequest{Query: "raw query words", Terms: []string{"parsed"}}
	if got := queryTermWords(req); len(got) != 1 || got[0] != "parsed" {
		t.Fatalf("parsed terms not preferred: %#v", got)
	}
	if got := queryTermWords(SearchRequest{Query: "raw query"}); len(got) != 2 {
		t.Fatalf("fallback words = %#v", got)
	}
}
