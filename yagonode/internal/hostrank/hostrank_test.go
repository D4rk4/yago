package hostrank

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestComputeRanksWellCitedHostHighest(t *testing.T) {
	// A -> H, A -> B, B -> H, C -> H. H is cited by three distinct hosts, B by
	// one, A and C by none, so authority must run H > B > {A, C}.
	table := Compute(map[string]map[string]int{
		"H": {"A": 1, "B": 1, "C": 1},
		"B": {"A": 1},
	})

	if got := table.Rank("H"); !almostEqual(got, 1) {
		t.Fatalf("top host rank = %v, want normalized 1", got)
	}
	if table.Rank("H") <= table.Rank("B") {
		t.Fatalf("H (%v) must outrank B (%v)", table.Rank("H"), table.Rank("B"))
	}
	if table.Rank("B") <= table.Rank("A") {
		t.Fatalf("B (%v) must outrank A (%v)", table.Rank("B"), table.Rank("A"))
	}
	if table.Rank("B") <= table.Rank("C") {
		t.Fatalf("B (%v) must outrank C (%v)", table.Rank("B"), table.Rank("C"))
	}
	if !almostEqual(table.Rank("A"), table.Rank("C")) {
		t.Fatalf("A (%v) and C (%v) have equal citations and must tie",
			table.Rank("A"), table.Rank("C"))
	}
}

func TestComputeIgnoresNonPositiveEdges(t *testing.T) {
	table := Compute(map[string]map[string]int{
		"H": {"A": 2, "Z": 0, "Q": -3},
	})

	if _, ok := table["Z"]; ok {
		t.Fatalf("zero-count edge admitted host Z: %v", table)
	}
	if _, ok := table["Q"]; ok {
		t.Fatalf("negative-count edge admitted host Q: %v", table)
	}
	if _, ok := table["H"]; !ok {
		t.Fatalf("cited host H missing: %v", table)
	}
	if _, ok := table["A"]; !ok {
		t.Fatalf("citing host A missing: %v", table)
	}
}

func TestComputeEmptyGraphIsEmptyTable(t *testing.T) {
	if got := Compute(nil); len(got) != 0 {
		t.Fatalf("nil graph = %v, want empty", got)
	}
	if got := Compute(map[string]map[string]int{"H": {"A": 0}}); len(got) != 0 {
		t.Fatalf("all-zero graph = %v, want empty", got)
	}
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
