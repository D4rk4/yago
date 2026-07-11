package searchcore

import "testing"

func TestDiversifyResultsKeepsUnclusteredSimilarDocuments(t *testing.T) {
	results := []Result{
		{URL: "https://a.example/1", Title: "Widget guide", Snippet: "same text"},
		{URL: "https://b.example/2", Title: "Widget guide", Snippet: "same text"},
	}

	diversified := DiversifyResults(results, Request{SiteHost: "example"})

	if len(diversified) != 2 ||
		diversified[0].URL != "https://a.example/1" ||
		diversified[1].URL != "https://b.example/2" {
		t.Fatalf("diversified = %#v", diversified)
	}
}

func TestDiversifyResultsDefersHostOverflow(t *testing.T) {
	results := []Result{
		{URL: "https://big.example/1", Host: "big.example"},
		{URL: "https://big.example/2", Host: "BIG.example"},
		{URL: "https://big.example/3", Host: "big.example"},
		{URL: "https://other.example/1", Host: "other.example"},
		{URL: "https://big.example/4", Host: "big.example"},
	}

	diversified := DiversifyResults(results, Request{})

	got := make([]string, 0, len(diversified))
	for _, result := range diversified {
		got = append(got, result.URL)
	}
	want := []string{
		"https://big.example/1",
		"https://big.example/2",
		"https://other.example/1",
		"https://big.example/3",
		"https://big.example/4",
	}
	if len(got) != len(want) {
		t.Fatalf("diversified = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestDiversifyResultsSkipsCrowdingForSiteAndDateQueries(t *testing.T) {
	results := []Result{
		{URL: "https://big.example/1", Host: "big.example"},
		{URL: "https://big.example/2", Host: "big.example"},
		{URL: "https://big.example/3", Host: "big.example"},
	}

	for _, req := range []Request{
		{SiteHost: "big.example"},
		{SortByDate: true},
	} {
		got := DiversifyResults(results, req)
		if len(got) != 3 || got[2].URL != "https://big.example/3" {
			t.Fatalf("crowding applied for %+v: %#v", req, got)
		}
	}
}

func TestDiversifyResultsKeepsHostlessResultsInPlace(t *testing.T) {
	results := []Result{
		{URL: "https://a.example/1"},
		{URL: "https://a.example/2"},
		{URL: "https://a.example/3"},
	}

	got := DiversifyResults(results, Request{})
	if len(got) != 3 || got[2].URL != "https://a.example/3" {
		t.Fatalf("hostless results must not be capped: %#v", got)
	}
}
