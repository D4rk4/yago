package searchcore

import "testing"

// An empty answer is either a fact about the index or a fact about the node's
// own health, and the wire shape is identical. Everything downstream keys on
// this predicate, so pin all four corners.
func TestUnprovenZeroSeparatesAnEmptyIndexFromAnUnansweredSearch(t *testing.T) {
	for _, item := range []struct {
		name     string
		response Response
		want     bool
	}{{
		name: "lost source over an empty result set",
		response: Response{
			PartialFailures: []PartialFailure{{Source: "fuzzy-stage", Reason: "deadline"}},
		},
		want: true,
	}, {
		name:     "every source answered and the index holds nothing",
		response: Response{Availability: ResultAvailability{Exhausted: true}},
		want:     false,
	}, {
		name:     "no failure recorded but availability never settled",
		response: Response{},
		want:     true,
	}, {
		name: "rows returned despite a lost source",
		response: Response{
			Results:         []Result{{URL: "https://example.org/doc"}},
			PartialFailures: []PartialFailure{{Source: "peer", Reason: "transport failed"}},
		},
		want: false,
	}} {
		t.Run(item.name, func(t *testing.T) {
			if got := item.response.UnprovenZero(); got != item.want {
				t.Fatalf("UnprovenZero() = %v, want %v", got, item.want)
			}
		})
	}
}
