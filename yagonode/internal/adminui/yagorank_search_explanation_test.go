package adminui

import (
	"context"
	"errors"
	"html"
	"strings"
	"testing"
)

type searchExplanationFixture struct {
	query       string
	global      bool
	explanation SearchExplanation
	err         error
}

func (f *searchExplanationFixture) Explain(
	_ context.Context,
	query string,
	global bool,
) (SearchExplanation, error) {
	f.query = query
	f.global = global

	return f.explanation, f.err
}

func TestConsoleYagoRankRendersSearchExplanation(t *testing.T) {
	source := &searchExplanationFixture{explanation: SearchExplanation{
		Query:         "alpha & beta",
		ModelRevision: "rank-7",
		ModelKind:     "linear_lambdarank",
		Results:       []SearchExplanationResult{completeSearchExplanationResult()},
	}}
	body := do(
		t,
		New(
			Options{
				Ranking:           &fakeRanking{profile: sampleRankingProfile()},
				SearchExplanation: source,
			},
		),
		"/admin/yagorank?q=%20alpha+%26+beta%20",
	).body
	body = html.UnescapeString(body)
	if source.query != "alpha & beta" {
		t.Fatalf("query = %q", source.query)
	}
	for _, want := range []string{
		"Search explain",
		`aria-label="Search explanation results"`,
		"1 bounded final result",
		"rank-7",
		"linear_lambdarank",
		"https://example.test/result",
		"7.250000",
		"5.000000",
		"Reciprocal-rank fusion",
		"strict",
		"Content quality",
		"strict_rank",
		"title BM25 contribution",
		"title_score",
		"+1.500000",
		"Tree 4 · lexical · contribution +0.400000",
		`/admin/search?p=1&q=alpha+%26+beta&scope=local`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("search explanation missing %q", want)
		}
	}
	if strings.Contains(body, "onclick=") {
		t.Fatal("search explanation introduced inline behavior")
	}
}

func completeSearchExplanationResult() SearchExplanationResult {
	return SearchExplanationResult{
		FinalRank:          1,
		URL:                "https://example.test/result",
		Source:             "local",
		Score:              7.25,
		RetrievalScore:     5,
		Quality:            0.8,
		QualityKnown:       true,
		SpamRisk:           0.1,
		SpamRiskKnown:      true,
		FunctionWordShare:  0.2,
		FunctionWordKnown:  true,
		SymbolShare:        0.3,
		SymbolKnown:        true,
		AlphabeticShare:    0.7,
		AlphabeticKnown:    true,
		UniqueTokenShare:   0.6,
		UniqueTokenKnown:   true,
		Proximity:          0.9,
		ProximityKnown:     true,
		FieldContributions: []SearchFieldContribution{{Name: "title", Score: 3.5}},
		Evidence:           []SearchRankingSignal{{Name: "strict_rank", Value: 1}},
		Fusion: []SearchFusionContribution{{
			Branch: "strict", Rank: 1, Contribution: 0.016393,
		}},
		RetrievalDiagnostic: "title BM25 contribution",
		Learned: &SearchLearnedExplanation{
			OriginalRank: 2, ModelRank: 1, FinalRank: 1, OriginalScore: 5, Score: 2,
			Signals: []SearchLearnedSignal{{
				Name: "title_score", Known: true, Value: 3.5, Used: true,
				NormalizedValue: 0.75, Weight: 2, Contribution: 1.5,
			}},
			Trees: []SearchLearnedTree{{
				Index: 4, InteractionGroup: "lexical", Contribution: 0.4,
				Decisions: []SearchLearnedTreeDecision{{
					Name: "title_score", Known: true, NormalizedValue: 0.75,
					Threshold: 0.5, WentLeft: false,
				}},
			}},
		},
	}
}

func TestConsoleYagoRankMarksMissingDiagnosticEvidenceUnknown(t *testing.T) {
	source := &searchExplanationFixture{explanation: SearchExplanation{
		Query: "alpha",
		Results: []SearchExplanationResult{{
			FinalRank: 1,
			URL:       "https://peer.example/",
			Source:    "peer",
		}},
	}}
	body := html.UnescapeString(do(
		t,
		New(Options{
			Ranking:           &fakeRanking{profile: sampleRankingProfile()},
			SearchExplanation: source,
		}),
		"/admin/yagorank?q=alpha&scope=global",
	).body)
	for _, signal := range []string{
		"Content quality",
		"Spam risk",
		"Function-word share",
		"Symbol share",
		"Alphabetic share",
		"Unique-token share",
		"Proximity",
	} {
		row := "<td>" + signal + "</td><td>no</td><td class=\"cds-mono\">—</td>"
		if !strings.Contains(body, row) {
			t.Fatalf("unknown diagnostic row missing %q", row)
		}
	}
}

func TestConsoleYagoRankSearchExplanationStates(t *testing.T) {
	ranking := &fakeRanking{profile: sampleRankingProfile()}
	withoutQuery := do(
		t,
		New(Options{Ranking: ranking, SearchExplanation: &searchExplanationFixture{}}),
		"/admin/yagorank",
	).body
	if !strings.Contains(withoutQuery, "Explain ranking") ||
		strings.Contains(withoutQuery, "bounded final result") {
		t.Fatalf("empty explain state = %s", withoutQuery)
	}

	failing := &searchExplanationFixture{err: errors.New("index failed")}
	failed := do(
		t,
		New(Options{Ranking: ranking, SearchExplanation: failing}),
		"/admin/yagorank?q=alpha",
	).body
	if !strings.Contains(failed, "Search explanation failed.") ||
		strings.Contains(failed, "index failed") {
		t.Fatalf("failed explain state = %s", failed)
	}

	withoutSource := do(t, New(Options{Ranking: ranking}), "/admin/yagorank?q=alpha").body
	if strings.Contains(withoutSource, "Search explain") {
		t.Fatalf("unwired explain rendered: %s", withoutSource)
	}
}

func TestConsoleSearchLinksToPrefilledExplanation(t *testing.T) {
	body := do(
		t,
		New(Options{
			Search:            &recordingSearch{},
			SearchExplanation: &searchExplanationFixture{},
		}),
		"/admin/search?q=alpha+%26+beta&scope=global",
	).body
	body = html.UnescapeString(body)
	if !strings.Contains(body, "Explain this ranking in YagoRank") ||
		!strings.Contains(body, `/admin/yagorank?q=alpha+%26+beta&scope=global`) {
		t.Fatalf("search explain handoff missing: %s", body)
	}
	local := html.UnescapeString(do(
		t,
		New(Options{
			Search:            &recordingSearch{},
			SearchExplanation: &searchExplanationFixture{},
		}),
		"/admin/search?q=alpha+%26+beta&scope=local",
	).body)
	if !strings.Contains(local, `/admin/yagorank?q=alpha+%26+beta&scope=local`) {
		t.Fatalf("local search explain handoff missing: %s", local)
	}
}

func TestConsoleYagoRankPreservesGlobalExplanationScope(t *testing.T) {
	source := &searchExplanationFixture{explanation: SearchExplanation{
		Query: "global", Global: true,
		PartialFailures: []string{"peer: remote search failed"},
	}}
	body := html.UnescapeString(do(
		t,
		New(
			Options{
				Ranking:           &fakeRanking{profile: sampleRankingProfile()},
				SearchExplanation: source,
			},
		),
		"/admin/yagorank?q=global&scope=global",
	).body)
	for _, want := range []string{
		`name="scope" value="global" checked`,
		"all enabled sources",
		"Partial failure: peer: remote search failed",
		`/admin/search?p=1&q=global&scope=global`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("global explanation missing %q: %s", want, body)
		}
	}
	if !source.global {
		t.Fatal("global scope was not forwarded")
	}
}
