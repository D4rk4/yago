package searchindex

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestMemoryIncludeDomainNarrowsCandidatesBeforeStoredFiltering(t *testing.T) {
	documents := includeDomainCandidateDocuments()
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := SearchRequest{
		Query:         "official documentation",
		MaxResults:    10,
		IncludeDomain: []string{"", "postgresql.org"},
	}
	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(
		request,
		index.multilingual,
		index.analyzerScope,
	))
	searchRequest.Size = len(documents)
	candidates, err := index.index.SearchInContext(t.Context(), searchRequest)
	if err != nil {
		t.Fatal(err)
	}
	if candidates.Total != 3 {
		t.Fatalf("candidate total = %d, want 3", candidates.Total)
	}

	result, err := index.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	assertIncludedDomainResults(t, result)
}

func TestDiskIncludeDomainStaysWithinCompleteSearchBudget(t *testing.T) {
	documents := includeDomainCandidateDocuments()
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })

	result, _, err := index.searchCompleteHitsWithin(
		t.Context(),
		SearchRequest{
			Query:         "official documentation",
			MaxResults:    10,
			IncludeDomain: []string{"postgresql.org"},
		},
		len(documents),
		3,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertIncludedDomainResults(t, result)
}

func TestIncludeDomainCandidatePreservesIDNPortAndUnfilterableHosts(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://docs.проект.example:8443/reference",
			Title:         "Bounded reference",
		},
		{
			NormalizedURL: "https://_/reference",
			Title:         "Bounded reference",
		},
	}
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}

	idn, err := index.Search(t.Context(), SearchRequest{
		Query:         "bounded reference",
		MaxResults:    10,
		IncludeDomain: []string{"проект.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if idn.Total != 1 || idn.Results[0].URL != documents[0].NormalizedURL {
		t.Fatalf("IDN result = %#v", idn)
	}

	unfilterable, err := index.Search(t.Context(), SearchRequest{
		Query:         "bounded reference",
		MaxResults:    10,
		IncludeDomain: []string{"_"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if unfilterable.Total != 1 ||
		unfilterable.Results[0].URL != documents[1].NormalizedURL {
		t.Fatalf("unfilterable host result = %#v", unfilterable)
	}
}

func TestIncludeDomainCandidateMatchesIPv6Document(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://[2001:db8::1]/reference",
		Title:         "Bounded reference",
	}
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: []documentstore.Document{document}},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query:         "bounded reference",
		MaxResults:    10,
		IncludeDomain: []string{"2001:db8::1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Results[0].URL != document.NormalizedURL {
		t.Fatalf("IPv6 result = %#v", result)
	}
}

func TestIncludeDomainCandidateDoesNotChangeLexicalScores(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://docs.example/reference",
			Title:         "Official documentation",
		},
		{
			NormalizedURL: "https://docs.example/tutorial",
			Title:         "Documentation",
			ExtractedText: "The official reference is linked from this tutorial.",
		},
	}
	index, err := NewBleveMemoryIndex(
		t.Context(),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := SearchRequest{Query: "official documentation", MaxResults: 10}
	unfiltered, err := index.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.IncludeDomain = []string{"docs.example"}
	filtered, err := index.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(unfiltered.Results) != 2 || len(filtered.Results) != 2 {
		t.Fatalf(
			"result sizes = %d/%d, want 2/2",
			len(unfiltered.Results),
			len(filtered.Results),
		)
	}
	for position := range unfiltered.Results {
		if filtered.Results[position].URL != unfiltered.Results[position].URL ||
			math.Abs(filtered.Results[position].Score-unfiltered.Results[position].Score) > 1e-12 {
			t.Fatalf(
				"filtered result %d = %#v, want lexical result %#v",
				position,
				filtered.Results[position],
				unfiltered.Results[position],
			)
		}
	}
}

func includeDomainCandidateDocuments() []documentstore.Document {
	documents := make([]documentstore.Document, 0, 35)
	for ordinal := range 32 {
		documents = append(documents, documentstore.Document{
			NormalizedURL: fmt.Sprintf("https://distractor-%d.example/reference", ordinal),
			Title:         "PostgreSQL official documentation",
		})
	}
	documents = append(
		documents,
		documentstore.Document{
			NormalizedURL: "https://postgresql.org/docs/current/",
			Title:         "PostgreSQL official documentation",
		},
		documentstore.Document{
			NormalizedURL: "https://www.postgresql.org/docs/current/",
			Title:         "PostgreSQL official documentation",
		},
		documentstore.Document{
			NormalizedURL: "https://postgresql.org.example/reference",
			Title:         "PostgreSQL official documentation",
		},
	)

	return documents
}

func assertIncludedDomainResults(t *testing.T, result SearchResultSet) {
	t.Helper()
	if result.Total != 2 || len(result.Results) != 2 {
		t.Fatalf("result size = %d/%d, want 2/2", len(result.Results), result.Total)
	}
	for _, item := range result.Results {
		if item.URL != "https://postgresql.org/docs/current/" &&
			item.URL != "https://www.postgresql.org/docs/current/" {
			t.Fatalf("unexpected included-domain result %q", item.URL)
		}
	}
}
