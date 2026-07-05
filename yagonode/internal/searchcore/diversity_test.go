package searchcore

import "testing"

func longSnippet(prefix string) string {
	return prefix + " quick brown fox jumps over the lazy dog near the river bank today"
}

func TestDiversifyResultsDropsNearDuplicates(t *testing.T) {
	results := []Result{
		// A mirror: the same content served from a different URL.
		{URL: "https://a.example/1", Title: "Widget guide", Snippet: longSnippet("alpha")},
		{URL: "https://b.example/2", Title: "Widget guide", Snippet: longSnippet("alpha")},
		// The same tokens reshuffled: SimHash is order-independent.
		{
			URL:     "https://d.example/4",
			Title:   "guide Widget",
			Snippet: "alpha brown quick fox jumps the over lazy dog near the river bank today",
		},
		{
			URL:     "https://c.example/3",
			Title:   "Something else",
			Snippet: longSnippet("totally different topic entirely unrelated"),
		},
	}

	diversified := DiversifyResults(results, Request{})

	if len(diversified) != 2 ||
		diversified[0].URL != "https://a.example/1" ||
		diversified[1].URL != "https://c.example/3" {
		t.Fatalf("diversified = %#v", diversified)
	}
}

func TestDiversifyResultsKeepsShortTextsApart(t *testing.T) {
	results := []Result{
		{URL: "https://a.example/1", Title: "Go"},
		{URL: "https://b.example/2", Title: "Go"},
		{URL: "https://c.example/3"},
		{URL: "https://d.example/4"},
	}

	if got := DiversifyResults(results, Request{}); len(got) != 4 {
		t.Fatalf("short texts must not dedupe: %#v", got)
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
