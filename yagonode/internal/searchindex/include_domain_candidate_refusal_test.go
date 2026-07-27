package searchindex

import (
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// The include-domain pre-filter narrows candidates only when every requested
// domain is something the URL analyzer can tokenize. One unfilterable domain
// disables the narrowing for the whole list, because a bleve clause that
// tokenizes to nothing matches nothing: it would drop the documents the
// post-retrieval filter would have admitted, and the request would honestly
// report zero hits for a domain the node does hold. The pre-filter is an
// optimization and may never decide the result set on its own. A blank entry is
// a different case and only skips itself.
func TestIncludeDomainCandidateQueryAbandonsNarrowingForAnyUnfilterableDomain(t *testing.T) {
	if query := bleveIncludeDomainCandidateQuery(
		[]string{"docs.example", "_"},
	); query != nil {
		t.Fatalf("mixed include-domain candidate query = %#v, want no narrowing", query)
	}
	if query := bleveIncludeDomainCandidateQuery(
		[]string{"docs.example", " . "},
	); query == nil {
		t.Fatal("blank include-domain entry disabled narrowing for a filterable list")
	}
	main := bleve.NewMatchAllQuery()
	if got := bleveQueryWithIncludeDomainCandidates(
		main,
		[]string{"docs.example", "_"},
	); got != main {
		t.Fatalf("unnarrowed query = %#v, want the lexical query unchanged", got)
	}
}

// End to end: a request naming both a tokenizable host and an unfilterable one
// must return both documents. Without the refusal the second document never
// reaches the post-filter that would have kept it.
func TestSearchKeepsUnfilterableHostAlongsideFilterableIncludeDomain(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://docs.example/reference",
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

	result, err := index.Search(t.Context(), SearchRequest{
		Query:         "bounded reference",
		MaxResults:    10,
		IncludeDomain: []string{"docs.example", "_"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != len(documents) || len(result.Results) != len(documents) {
		t.Fatalf("mixed include-domain result = %#v, want both documents", result)
	}
	found := map[string]bool{}
	for _, item := range result.Results {
		found[item.URL] = true
	}
	for _, document := range documents {
		if !found[document.NormalizedURL] {
			t.Fatalf("include-domain pre-filter dropped %q", document.NormalizedURL)
		}
	}
}
