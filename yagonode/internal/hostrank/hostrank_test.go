package hostrank

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestRankReturnsZeroForUnknownOrNilTable(t *testing.T) {
	table := AuthorityTable{"x": {Score: 0.7}}
	if got := table.Rank("x"); !almostEqual(got, 0.7) {
		t.Fatalf("known host rank = %v, want 0.7", got)
	}
	if got := table.Rank("missing"); got != 0 {
		t.Fatalf("unknown host rank = %v, want 0", got)
	}
	if got := AuthorityTable(nil).Rank("x"); got != 0 {
		t.Fatalf("nil-table rank = %v, want 0", got)
	}
}
