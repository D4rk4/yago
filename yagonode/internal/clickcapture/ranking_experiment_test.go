package clickcapture

import (
	"reflect"
	"testing"
)

func TestAdjacentPairRandomizationIsDeterministicAndBounded(t *testing.T) {
	candidates := experimentCandidates("a", "b", "c", "d", "e")
	first := AdjacentPairRandomization(candidates, 17)
	second := AdjacentPairRandomization(candidates, 17)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("adjacent-pair randomization is not deterministic")
	}
	seen := map[string]bool{}
	paired := 0
	for index, displayed := range first {
		seen[displayed.URLIdentity] = true
		if displayed.Position != index+1 {
			t.Fatalf("position %d = %d", index, displayed.Position)
		}
		if displayed.Propensity == 0.5 {
			paired++
		} else if displayed.Propensity != 0 || displayed.Attribution != AttributionFixed {
			t.Fatalf("unpaired candidate = %#v", displayed)
		}
	}
	if len(seen) != len(candidates) || paired != 4 {
		t.Fatalf("seen=%d paired=%d", len(seen), paired)
	}
	if !reflect.DeepEqual(candidates, experimentCandidates("a", "b", "c", "d", "e")) {
		t.Fatal("adjacent-pair randomization mutated its input")
	}
}

func TestAdjacentPairRandomizationFairness(t *testing.T) {
	candidates := experimentCandidates("a", "b", "c", "d")
	pairedAtFirst := 0
	swapped := 0
	original := 0
	for seed := range uint64(2000) {
		displayed := AdjacentPairRandomization(candidates, seed)
		if displayed[0].Propensity == 0.5 {
			pairedAtFirst++
		}
		for _, candidate := range displayed {
			switch candidate.Attribution {
			case AttributionSwapped:
				swapped++
			case AttributionOriginal:
				original++
			}
		}
	}
	assertFairShare(t, pairedAtFirst, 2000)
	assertFairShare(t, swapped, swapped+original)
}

func TestTeamDraftInterleaveAttributesAndDeduplicates(t *testing.T) {
	primary := experimentCandidates("a", "b", "c", "d")
	secondary := experimentCandidates("b", "a", "e", "f")
	first := TeamDraftInterleave(primary, secondary, 42, 5)
	second := TeamDraftInterleave(primary, secondary, 42, 5)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("team draft is not deterministic")
	}
	seen := map[string]bool{}
	primaryContributions := 0
	secondaryContributions := 0
	for index, candidate := range first {
		if seen[candidate.ClusterIdentity] {
			t.Fatalf("duplicate candidate: %#v", candidate)
		}
		seen[candidate.ClusterIdentity] = true
		if candidate.Position != index+1 || candidate.Propensity != 0 {
			t.Fatalf("draft position or propensity = %#v", candidate)
		}
		switch candidate.Attribution {
		case AttributionPrimary:
			primaryContributions++
		case AttributionSecondary:
			secondaryContributions++
		default:
			t.Fatalf("unknown attribution %q", candidate.Attribution)
		}
	}
	if len(first) != 5 || primaryContributions-secondaryContributions > 1 ||
		secondaryContributions-primaryContributions > 1 {
		t.Fatalf(
			"draft balance primary=%d secondary=%d length=%d",
			primaryContributions,
			secondaryContributions,
			len(first),
		)
	}
	if got := TeamDraftInterleave(primary, secondary, 1, 0); got != nil {
		t.Fatalf("zero-limit draft = %#v", got)
	}
}

func TestTeamDraftFirstPickFairnessAndExhaustion(t *testing.T) {
	primary := experimentCandidates("a")
	secondary := experimentCandidates("b")
	primaryFirst := 0
	for seed := range uint64(2000) {
		draft := TeamDraftInterleave(primary, secondary, seed, 2)
		if draft[0].Attribution == AttributionPrimary {
			primaryFirst++
		}
	}
	assertFairShare(t, primaryFirst, 2000)
	onlyPrimary := TeamDraftInterleave(primary, nil, 3, 4)
	if len(onlyPrimary) != 1 || onlyPrimary[0].URLIdentity != "a" {
		t.Fatalf("exhausted secondary draft = %#v", onlyPrimary)
	}
}

func TestCandidateIdentityFallback(t *testing.T) {
	if got := candidateIdentity(Candidate{URLIdentity: "url"}); got != "url" {
		t.Fatalf("URL fallback identity = %q", got)
	}
	if got := candidateIdentity(
		Candidate{URLIdentity: "url", ClusterIdentity: "cluster"},
	); got != "cluster" {
		t.Fatalf("cluster identity = %q", got)
	}
}

func assertFairShare(t *testing.T, selected, total int) {
	t.Helper()
	share := float64(selected) / float64(total)
	if share < 0.45 || share > 0.55 {
		t.Fatalf("fair share = %.4f (%d/%d)", share, selected, total)
	}
}

func experimentCandidates(identities ...string) []Candidate {
	candidates := make([]Candidate, len(identities))
	for index, identity := range identities {
		candidates[index] = Candidate{
			URLIdentity:     identity,
			ClusterIdentity: identity,
			Position:        index + 1,
		}
	}

	return candidates
}
