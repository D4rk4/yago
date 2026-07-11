package searchcore

import "testing"

func TestOrderByDateWhenRequestedSortsNewestFirst(t *testing.T) {
	results := []Result{
		{URL: "a", Date: "20250101", Score: 5},
		{URL: "undated-1", Score: 4},
		{URL: "b", Date: "20260630", Score: 3},
		{URL: "undated-2", Score: 2},
		{URL: "c", Date: "20260101", Score: 1},
	}

	OrderByDateWhenRequested(results, Request{SortByDate: true})

	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.URL)
	}
	want := []string{"b", "c", "a", "undated-1", "undated-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestOrderByDateWhenRequestedKeepsRelevanceOrderWithoutModifier(t *testing.T) {
	results := []Result{
		{URL: "low", Date: "20200101"},
		{URL: "high", Date: "20260101"},
	}

	OrderByDateWhenRequested(results, Request{})

	if results[0].URL != "low" || results[1].URL != "high" {
		t.Fatalf("order changed without the /date modifier: %#v", results)
	}
}

func TestFinalRankingStageOwnsFederatedDateSort(t *testing.T) {
	local := &fakeCoreSearcher{response: Response{Results: []Result{
		{URL: "a-old", URLHash: "a-old", Date: "20240101", Score: 10},
	}}}
	remote := &fakeCoreSearcher{response: Response{Results: []Result{
		{URL: "z-new", URLHash: "z-new", Date: "20260101", Score: 0.1},
	}}}

	federated := NewFederatedSearcher(local, remote)
	req := Request{Source: SourceGlobal, Limit: 10, SortByDate: true}
	raw, err := federated.Search(
		t.Context(),
		req,
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(raw.Results) != 2 || raw.Results[0].URL != "a-old" {
		t.Fatalf("federated candidate order = %#v", raw.Results)
	}

	resp, err := NewLexicalRerankSearcher(federated).Search(t.Context(), req)
	if err != nil {
		t.Fatalf("final Search: %v", err)
	}
	if len(resp.Results) != 2 || resp.Results[0].URL != "z-new" {
		t.Fatalf("results = %#v, want the newest first despite lower score", resp.Results)
	}
}
