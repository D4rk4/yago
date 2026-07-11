package searchcore

import (
	"reflect"
	"testing"
)

func TestLexicalPositionsPreserveForeignSlotsAndRestoreLocalOrder(t *testing.T) {
	local := func(url string, rank float64) Result {
		return Result{
			URL: url,
			Evidence: NewRankingEvidence(RankingSignalValue{
				Signal: SignalLocalRank,
				Value:  rank,
			}),
		}
	}
	results := []Result{
		local("three", 3),
		local("one", 1),
		{URL: "peer", Source: SourceRemote},
		local("two", 2),
		{URL: "unknown"},
	}
	if got := LexicalPositions(results, 10); !reflect.DeepEqual(got, []int{14, 11, 13, 12, 15}) {
		t.Fatalf("lexical positions = %v", got)
	}
	if got := LexicalPositions(nil, 10); got == nil || len(got) != 0 {
		t.Fatalf("empty lexical positions = %#v", got)
	}
	if got := LexicalPositions([]Result{
		{URL: "unknown"}, local("known", 1), local("tie", 1),
	}, 0); !reflect.DeepEqual(got, []int{3, 1, 2}) {
		t.Fatalf("known-first lexical positions = %v", got)
	}
}
