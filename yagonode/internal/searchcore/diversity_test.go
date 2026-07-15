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

func TestDiversifyResultsDefersSiteOverflow(t *testing.T) {
	results := []Result{
		{URL: "https://example.com/1", Host: "Example.COM."},
		{URL: "https://www.example.com/2", Host: "WWW.example.com"},
		{URL: "https://docs.example.com/3", Host: "docs.example.com"},
		{URL: "https://other.net/1", Host: "other.net"},
		{URL: "https://blog.example.com/4", Host: "blog.example.com"},
	}

	diversified := DiversifyResults(results, Request{})

	got := make([]string, 0, len(diversified))
	for _, result := range diversified {
		got = append(got, result.URL)
	}
	want := []string{
		"https://example.com/1",
		"https://www.example.com/2",
		"https://other.net/1",
		"https://docs.example.com/3",
		"https://blog.example.com/4",
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
		{URL: "https://example.com/1", Host: "example.com"},
		{URL: "https://www.example.com/2", Host: "www.example.com"},
		{URL: "https://docs.example.com/3", Host: "docs.example.com"},
	}

	for _, req := range []Request{
		{SiteHost: "example.com"},
		{SortByDate: true},
	} {
		got := DiversifyResults(results, req)
		if len(got) != 3 || got[2].URL != "https://docs.example.com/3" {
			t.Fatalf("crowding applied for %+v: %#v", req, got)
		}
	}
}

func TestDiversifyResultsKeepsDifferentSitesIndependent(t *testing.T) {
	results := []Result{
		{URL: "https://www.alpha.com/1", Host: "www.alpha.com"},
		{URL: "https://docs.alpha.com/2", Host: "docs.alpha.com"},
		{URL: "https://www.beta.com/1", Host: "www.beta.com"},
		{URL: "https://docs.beta.com/2", Host: "docs.beta.com"},
	}

	got := DiversifyResults(results, Request{})
	if len(got) != len(results) {
		t.Fatalf("result length = %d, want %d", len(got), len(results))
	}
	for i := range results {
		if got[i].URL != results[i].URL {
			t.Fatalf("order = %#v", got)
		}
	}
}

func TestDiversifyResultsKeepsIPAddressesIndependent(t *testing.T) {
	results := []Result{
		{URL: "https://192.0.2.1/1", Host: "192.0.2.1"},
		{URL: "https://192.0.2.1/2", Host: "192.0.2.1"},
		{URL: "https://198.51.2.1/1", Host: "198.51.2.1"},
		{URL: "https://192.0.2.1/3", Host: "192.0.2.1"},
	}

	got := DiversifyResults(results, Request{})
	want := []string{
		"https://192.0.2.1/1",
		"https://192.0.2.1/2",
		"https://198.51.2.1/1",
		"https://192.0.2.1/3",
	}
	for i := range want {
		if got[i].URL != want[i] {
			t.Fatalf("order = %#v", got)
		}
	}
}

func TestDiversifyResultsUsesExactInvalidHostIdentity(t *testing.T) {
	results := []Result{
		{URL: "invalid-1", Host: "Invalid Host"},
		{URL: "invalid-2", Host: "invalid host"},
		{URL: "invalid-3", Host: "INVALID HOST"},
		{URL: "localhost", Host: "localhost"},
	}

	got := DiversifyResults(results, Request{})
	want := []string{"invalid-1", "invalid-2", "localhost", "invalid-3"}
	for i := range want {
		if got[i].URL != want[i] {
			t.Fatalf("order = %#v", got)
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
