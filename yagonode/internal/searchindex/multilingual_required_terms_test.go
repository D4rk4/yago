package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestCJKTermRequiresEveryAnalyzedComponent(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://example.org/partial",
				Title:         "東京",
				ExtractedText: "東京",
				Language:      "zh",
			},
			{
				NormalizedURL: "https://example.org/full",
				Title:         "東京大学",
				ExtractedText: "東京大学",
				Language:      "zh",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "東京大学", Terms: []string{"東京大学"}, MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].URL != "https://example.org/full" {
		t.Fatalf("results = %#v", result.Results)
	}
}

func TestLatinQueriesReachEveryRegisteredStemmer(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/finnish",
			Title:         "Suomalainen teksti",
			ExtractedText: "Perheet asuvat talossa rauhallisella kadulla.",
			Language:      "fi",
		},
		{
			NormalizedURL: "https://example.org/turkish",
			Title:         "Turkce metin",
			ExtractedText: "Okurlar kitaplarda yeni fikirler bulurlar.",
			Language:      "tr",
		},
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{documents: documents})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	for _, test := range []struct {
		query string
		url   string
	}{
		{query: "talot", url: "https://example.org/finnish"},
		{query: "kitaplar", url: "https://example.org/turkish"},
	} {
		result, err := index.Search(t.Context(), SearchRequest{
			Query: test.query, Terms: []string{test.query}, MaxResults: 10,
		})
		if err != nil {
			t.Fatalf("Search(%q): %v", test.query, err)
		}
		if len(result.Results) != 1 || result.Results[0].URL != test.url {
			t.Fatalf("Search(%q) = %#v", test.query, result.Results)
		}
	}
}

func TestOperatorBearingQueryUsesParsedTermsForRussianMorphologyEvidence(t *testing.T) {
	host := "government-information-archive-for-montenegro.example.org"
	doc := documentstore.Document{
		NormalizedURL: "https://" + host + "/ru/official-report",
		Title:         "Official report",
		ExtractedText: strings.Repeat("официальный материал раздела ", 30) +
			"сведения о Черногории опубликованы в полном объеме",
		Language: "ru",
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{doc},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:         "черногория site:" + host,
		Terms:         []string{"черногория"},
		IncludeDomain: []string{host},
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("results = %#v", result.Results)
	}
	if !result.Results[0].EvidenceReady ||
		!strings.Contains(result.Results[0].Snippet, "Черногории опубликованы") {
		t.Fatalf("result = %#v", result.Results[0])
	}
}
