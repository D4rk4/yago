package websearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type webAnswerRecoveryCase struct {
	name              string
	newSearcher       func(searchcore.Searcher, Provider) searchcore.Searcher
	primary           searchcore.Response
	wantResults       int
	wantRecovered     string
	wantDidYouMean    string
	wantPrimaryResult bool
}

func TestWebAnswerReconcilesPrimaryRecoveryMetadata(t *testing.T) {
	tests := []webAnswerRecoveryCase{
		{
			name: "miss triggered web only",
			newSearcher: func(primary searchcore.Searcher, provider Provider) searchcore.Searcher {
				return NewFallbackSearcher(primary, provider, enabled)
			},
			primary:     searchcore.Response{DidYouMean: "golang"},
			wantResults: 1,
		},
		{
			name: "always web only",
			newSearcher: func(primary searchcore.Searcher, provider Provider) searchcore.Searcher {
				return NewParallelSearcher(primary, provider, enabled)
			},
			primary: searchcore.Response{
				TotalResults: 37,
				Recovered:    "fuzzy",
				DidYouMean:   "golang",
			},
			wantResults: 1,
		},
		{
			name: "always fuzzy primary and web",
			newSearcher: func(primary searchcore.Searcher, provider Provider) searchcore.Searcher {
				return NewParallelSearcher(primary, provider, enabled)
			},
			primary: searchcore.Response{
				TotalResults: 1,
				Results: []searchcore.Result{{
					Title: "Golang local answer", URL: "https://local.example/golang",
					Source: searchcore.SourceLocal,
				}},
				Recovered: "fuzzy", DidYouMean: "golang",
			},
			wantResults:       2,
			wantRecovered:     "fuzzy",
			wantDidYouMean:    "golang",
			wantPrimaryResult: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertWebAnswerRecovery(t, test)
		})
	}
}

func assertWebAnswerRecovery(t *testing.T, test webAnswerRecoveryCase) {
	t.Helper()
	provider := &stubProvider{results: []Result{{
		Title: "Golnag web answer", URL: "https://web.example/golnag",
	}}}
	response, err := test.newSearcher(
		&stubSearcher{resp: test.primary},
		provider,
	).Search(context.Background(), searchcore.Request{
		Query: "golnag", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != test.wantResults ||
		response.TotalResults != test.wantResults {
		t.Fatalf("response = %#v", response)
	}
	if response.Recovered != test.wantRecovered ||
		response.DidYouMean != test.wantDidYouMean {
		t.Fatalf(
			"recovery = %q suggestion = %q",
			response.Recovered,
			response.DidYouMean,
		)
	}
	foundWeb := false
	foundPrimary := false
	for _, result := range response.Results {
		foundWeb = foundWeb || result.Source == searchcore.SourceWeb
		foundPrimary = foundPrimary || result.Source == searchcore.SourceLocal
	}
	if !foundWeb || foundPrimary != test.wantPrimaryResult || provider.calls != 1 {
		t.Fatalf(
			"sources = %#v provider calls = %d",
			response.Results,
			provider.calls,
		)
	}
}
