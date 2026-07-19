package resultreason

import (
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestReasonsUseBoundedRankingEvidence(t *testing.T) {
	result := searchcore.Result{
		Source: searchcore.SourceLocal,
		Evidence: searchcore.NewRankingEvidence(
			searchcore.RankingSignalValue{Signal: searchcore.SignalTitleScore, Value: 2},
			searchcore.RankingSignalValue{Signal: searchcore.SignalHeadingScore, Value: 1},
			searchcore.RankingSignalValue{Signal: searchcore.SignalOrderedProximity, Value: 0.8},
			searchcore.RankingSignalValue{Signal: searchcore.SignalAuthority, Value: 0.4},
			searchcore.RankingSignalValue{Signal: searchcore.SignalFreshness, Value: 0.2},
			searchcore.RankingSignalValue{Signal: searchcore.SignalSourceCount, Value: 3},
		),
	}

	reasons := For(result)
	if len(reasons) != Maximum {
		t.Fatalf("reasons = %#v", reasons)
	}
	for _, want := range []string{
		"Matched the local full-text index.",
		"The query matched the title.",
		"The query matched a heading.",
		"The query words appear in order and close together.",
		"Links from other indexed pages contributed authority.",
		"Document freshness contributed to this rank.",
	} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("reasons %q do not contain %q", reasons, want)
		}
	}
}

func TestReasonsLabelProvenanceWithoutUnknownSignals(t *testing.T) {
	peer := For(searchcore.Result{Source: searchcore.SourceRemote})
	web := For(searchcore.Result{Source: searchcore.SourceWeb})
	supported := For(searchcore.Result{Evidence: searchcore.NewRankingEvidence(
		searchcore.RankingSignalValue{Signal: searchcore.SignalSourceCount, Value: 2},
	)})
	if len(peer) != 1 || !strings.Contains(peer[0], "YaCy peers") {
		t.Fatalf("peer reasons = %#v", peer)
	}
	if len(web) != 1 || !strings.Contains(web[0], "web fallback") {
		t.Fatalf("web reasons = %#v", web)
	}
	if len(supported) != 2 || !strings.Contains(supported[1], "retrieval source") {
		t.Fatalf("multi-source reasons = %#v", supported)
	}
}
