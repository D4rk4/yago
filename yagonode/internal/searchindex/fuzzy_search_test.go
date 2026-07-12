package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestFuzzySearchToleratesOneEditPerTerm(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/golang",
			Title:         "Golang tutorial",
			ExtractedText: "Learning golang from scratch.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	exact, err := index.Search(t.Context(), SearchRequest{Query: "golnaX", MaxResults: 5})
	if err != nil {
		t.Fatalf("exact search: %v", err)
	}
	if exact.Total != 0 {
		t.Fatalf("misspelling matched without fuzzy: %#v", exact)
	}

	fuzzy, err := index.Search(t.Context(), SearchRequest{
		Query:      "golanX",
		MaxResults: 5,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	if fuzzy.Total != 1 || len(fuzzy.Results) != 1 {
		t.Fatalf("fuzzy search missed the close match: %#v", fuzzy)
	}
}

func TestFuzzySearchRequiresEveryQueryTerm(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/golang",
			Title:         "Golang tutorial",
			ExtractedText: "Learning golang from scratch.",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "golanX missinZ",
		Terms:      []string{"golanX", "missinZ"},
		MaxResults: 5,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("partial fuzzy match admitted a document: %#v", result)
	}
}

func TestFuzzySearchRejectsDispersedQueryTrigrams(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/noise",
			Title:         "Unrelated page",
			ExtractedText: "пси лоб оба бат аты",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "псилобаты",
		Terms:      []string{"псилобаты"},
		MaxResults: 5,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	if result.Total != 0 {
		t.Fatalf("dispersed trigrams admitted a document: %#v", result)
	}
}

func TestFuzzySearchRejectsASeparateLongWord(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/psychology",
			Title:         "Литературное исследование",
			ExtractedText: strings.Repeat("Вводный материал без нужного слова. ", 30) +
				"Исследование о психопатах в художественных произведениях.",
			Language: "ru",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "псилобаты",
		Terms:      []string{"псилобаты"},
		MaxResults: 5,
		Fuzzy:      true,
	})
	if err != nil {
		t.Fatalf("fuzzy search: %v", err)
	}
	if result.Total != 0 || len(result.Results) != 0 {
		t.Fatalf("separate word was admitted as a typo: %#v", result)
	}
}

func TestFuzzyRecoveryQueryAcceptsAnUnparsedEmptyRequest(t *testing.T) {
	query := fuzzyRecoveryQuery(
		SearchRequest{},
		[]string{standardTextAnalyzer},
		DefaultRankingWeights(),
		true,
	)
	if query == nil {
		t.Fatal("empty fuzzy request produced no query")
	}
}

func TestFuzzyRecoveryBoundsDependOnTermLength(t *testing.T) {
	tests := []struct {
		term     string
		prefix   int
		distance int
	}{
		{term: "at", prefix: 0, distance: 0},
		{term: "cat", prefix: 0, distance: 1},
		{term: "term", prefix: 1, distance: 1},
		{term: "search", prefix: 2, distance: 1},
		{term: "searcher", prefix: 4, distance: 2},
		{term: "поиск", prefix: len("п"), distance: 1},
		{term: "псилобаты", prefix: len("псил"), distance: 2},
		{term: strings.Repeat("a", maximumFuzzyTermRunes+1), prefix: 2, distance: 0},
	}
	for _, test := range tests {
		if got := fuzzyPrefixLength(test.term); got != test.prefix {
			t.Errorf("prefix for %q = %d, want %d", test.term, got, test.prefix)
		}
		if got := fuzzyEditDistance(test.term); got != test.distance {
			t.Errorf("distance for %q = %d, want %d", test.term, got, test.distance)
		}
	}
}

func TestRussianAnalyzerRecallsPsilobatyInflection(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{{
			NormalizedURL: "https://a.example/poem",
			Title:         "Старинное слово",
			ExtractedText: strings.Repeat("Вводный материал без нужного слова. ", 30) +
				"История о псилобатах и канатоходцах.",
			Language: "ru",
		}},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "псилобаты",
		Terms:      []string{"псилобаты"},
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("Russian inflection was not recalled: %#v", result)
	}
	if !strings.Contains(result.Results[0].Snippet, "псилобатах") {
		t.Fatalf("morphology snippet missed matching form: %q", result.Results[0].Snippet)
	}
}
