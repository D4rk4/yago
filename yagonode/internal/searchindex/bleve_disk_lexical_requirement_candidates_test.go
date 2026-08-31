package searchindex

import (
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestBleveLexicalCandidatesRequireEveryStrictTerm(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/both",
			Title:         "Specializedterm Regionalterm",
			Language:      "hr",
		},
		{
			NormalizedURL: "https://example.org/city",
			Title:         "Regionalterm guide",
			Language:      "hr",
		},
		{
			NormalizedURL: "https://example.org/bank",
			Title:         "Specializedterm guide",
			Language:      "hr",
		},
	}
	request := SearchRequest{
		Query:      "specializedterm regionalterm",
		Terms:      []string{"specializedterm", "regionalterm"},
		MaxResults: len(documents),
	}
	candidates := lexicalCandidateIdentities(t, documents, request)
	if !candidates[documentID(documents[0])] ||
		candidates[documentID(documents[1])] ||
		candidates[documentID(documents[2])] ||
		len(candidates) != 1 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestBleveLexicalCandidatesHonorRelaxedTermMinimum(t *testing.T) {
	documents := []documentstore.Document{
		{NormalizedURL: "https://example.org/all", Title: "alpha beta gamma"},
		{NormalizedURL: "https://example.org/two", Title: "alpha beta"},
		{NormalizedURL: "https://example.org/one", Title: "alpha"},
	}
	request := SearchRequest{
		Query: "alpha beta gamma", Terms: []string{"alpha", "beta", "gamma"},
		MaxResults: len(documents), Relaxed: true,
	}
	candidates := lexicalCandidateIdentities(t, documents, request)
	if !candidates[documentID(documents[0])] ||
		!candidates[documentID(documents[1])] ||
		candidates[documentID(documents[2])] ||
		len(candidates) != 2 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestBleveLexicalCandidatesRetainAnalyzerStopwordMatches(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/cat",
			Title:         "Quiet cat",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/article",
			Title:         "The article",
			Language:      "en",
		},
	}
	request := SearchRequest{
		Query: "the cat", Terms: []string{"the", "cat"}, MaxResults: len(documents),
	}
	candidates := lexicalCandidateIdentities(t, documents, request)
	if !candidates[documentID(documents[0])] ||
		candidates[documentID(documents[1])] ||
		len(candidates) != 1 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestBleveLexicalCandidatesRetainStandardAllStopwordMatch(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/the",
			Title:         "The article",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/other",
			Title:         "Other article",
			Language:      "en",
		},
	}
	request := SearchRequest{Query: "the", Terms: []string{"the"}, MaxResults: len(documents)}
	candidates := lexicalCandidateIdentities(t, documents, request)
	if !candidates[documentID(documents[0])] ||
		candidates[documentID(documents[1])] ||
		len(candidates) != 1 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestBleveLexicalCandidateAnalyzersPreserveLegacyAndUnavailableMappings(t *testing.T) {
	request := SearchRequest{Query: "needle", Terms: []string{"needle"}}
	if analyzers := lexicalRequirementCandidateAnalyzers(request, false); !slices.Equal(
		analyzers,
		[]string{""},
	) {
		t.Fatalf("legacy analyzers = %v", analyzers)
	}

	original := loadStemmingMapping
	t.Cleanup(func() { loadStemmingMapping = original })
	loadStemmingMapping = func() *mapping.IndexMappingImpl { return nil }
	analyzers := lexicalRequirementCandidateAnalyzers(request, true)
	standard := 0
	for _, analyzer := range analyzers {
		if analyzer == standardTextAnalyzer {
			standard++
		}
	}
	if standard != 1 || len(analyzers) != len(queryAnalyzers(request.Query)) {
		t.Fatalf("unavailable analyzers = %v", analyzers)
	}
}

func TestBleveLexicalCandidatesRequireEveryFuzzyTerm(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://example.org/both",
			Title:         "Specializedterm Regionalterm",
		},
		{
			NormalizedURL: "https://example.org/bank",
			Title:         "Specializedterm Alternateplace",
		},
		{
			NormalizedURL: "https://example.org/city",
			Title:         "Regionalterm guide",
		},
	}
	request := SearchRequest{
		Query:      "specializedterm regionalterx",
		Terms:      []string{"specializedterm", "regionalterx"},
		MaxResults: len(documents), Fuzzy: true,
	}
	candidates := lexicalCandidateIdentities(t, documents, request)
	if !candidates[documentID(documents[0])] ||
		candidates[documentID(documents[1])] ||
		candidates[documentID(documents[2])] ||
		len(candidates) != 1 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func lexicalCandidateIdentities(
	t *testing.T,
	documents []documentstore.Document,
	request SearchRequest,
) map[string]bool {
	t.Helper()
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.index.Close() })
	searchRequest := bleve.NewSearchRequest(bleveLexicalCandidateQuery(
		request,
		index.multilingual,
	))
	searchRequest.Size = len(documents)
	result, err := index.index.SearchInContext(t.Context(), searchRequest)
	if err != nil {
		t.Fatal(err)
	}

	identities := make(map[string]bool, len(result.Hits))
	for _, hit := range result.Hits {
		identities[hit.ID] = true
	}

	return identities
}
