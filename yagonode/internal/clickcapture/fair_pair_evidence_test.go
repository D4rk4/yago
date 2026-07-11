package clickcapture

import (
	"strings"
	"testing"
)

func TestFairPairClickAccountingHandlesBothMembersAndBounds(t *testing.T) {
	pair := canonicalFairPair(
		Candidate{URLIdentity: "https://b/", ClusterIdentity: "b"},
		Candidate{URLIdentity: "https://a/", ClusterIdentity: "a"},
	)
	key := fairPairKey(pair.FirstCluster, pair.SecondCluster)
	model := ModelEvidence{FairPairs: map[string]FairPairEvidence{
		key: {
			FirstCluster: pair.FirstCluster, FirstURL: pair.FirstURL,
			SecondCluster: pair.SecondCluster, SecondURL: pair.SecondURL,
			Impressions: 1,
		},
	}}
	addFairPairClick(&model, "a", pair)
	addFairPairClick(&model, "b", pair)
	addFairPairClick(&model, "missing", pair)
	addFairPairClick(&model, "a", pair)
	addFairPairClick(&ModelEvidence{}, "a", pair)
	evidence := model.FairPairs[key]
	if evidence.FirstClicks != 1 || evidence.SecondClicks != 1 {
		t.Fatalf("pair clicks = %#v", evidence)
	}
}

func TestDisplayedFairPairRecognitionAndLookup(t *testing.T) {
	candidates := []DisplayedCandidate{
		candidateFixture(
			Candidate{URLIdentity: "fixed", ClusterIdentity: "fixed", Position: 3},
			0, AttributionFixed, 2,
		),
		candidateFixture(
			Candidate{URLIdentity: "second", ClusterIdentity: "second", Position: 2},
			0.5, AttributionSwapped, 1,
		),
		candidateFixture(
			Candidate{URLIdentity: "first", ClusterIdentity: "first", Position: 1},
			0.5, AttributionSwapped, 0,
		),
	}
	pairs := displayedFairPairs(candidates)
	if len(pairs) != 1 || pairs[0].FirstCluster != "first" ||
		pairs[0].SecondCluster != "second" {
		t.Fatalf("displayed pairs = %#v", pairs)
	}
	if pairedFairPairMember(candidates, candidates[0]) != nil {
		t.Fatal("fixed candidate acquired a pair")
	}
	missing := candidates[2]
	missing.OriginalIndex = 4
	if pairedFairPairMember(candidates, missing) != nil {
		t.Fatal("unpaired randomized candidate acquired a pair")
	}
	if pair := pairedFairPairMember(candidates, candidates[2]); pair == nil ||
		pair.SecondCluster != "second" {
		t.Fatalf("paired member = %#v", pair)
	}
	broken := append([]DisplayedCandidate(nil), candidates...)
	broken[1].Position = 4
	broken[2].Propensity = 0.25
	if len(displayedFairPairs(broken)) != 0 {
		t.Fatal("broken pair was recognized")
	}
}

func TestFairPairConfidenceAndValidation(t *testing.T) {
	first := pairEvidence(
		pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 20, 20, 0,
	)
	if cluster, _, score, ok := confidentFairPairWinner(first, 3); !ok ||
		cluster != "a" || score <= 0 {
		t.Fatalf("first winner = %q, %v, %v", cluster, score, ok)
	}
	second := pairEvidence(
		pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 20, 0, 20,
	)
	if cluster, _, score, ok := confidentFairPairWinner(second, 3); !ok ||
		cluster != "b" || score <= 0 {
		t.Fatalf("second winner = %q, %v, %v", cluster, score, ok)
	}
	for _, pair := range []FairPairEvidence{
		pairEvidence(
			pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 2, 2, 0,
		),
		pairEvidence(
			pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 20, 10, 10,
		),
	} {
		if _, _, _, ok := confidentFairPairWinner(pair, 3); ok {
			t.Fatalf("weak pair accepted = %#v", pair)
		}
	}
	lower, upper := wilsonInterval(0, 20)
	if lower != 0 || upper <= 0 || upper >= 0.5 || absInt(-2) != 2 || absInt(2) != 2 {
		t.Fatalf("confidence helpers = %v, %v", lower, upper)
	}
	valid := map[string]FairPairEvidence{fairPairKey("a", "b"): first}
	if err := validateFairPairEvidence(valid); err != nil {
		t.Fatalf("valid fair pair: %v", err)
	}
	tooMany := make(map[string]FairPairEvidence, maximumResultsPerModel+1)
	for index := range maximumResultsPerModel + 1 {
		key := string(rune(index + 1000))
		tooMany[key] = first
	}
	invalid := []map[string]FairPairEvidence{
		tooMany,
		{"wrong": first},
		{fairPairKey("a", "b"): {
			FirstCluster: "b", FirstURL: "a", SecondCluster: "a", SecondURL: "b",
			Impressions: 1,
		}},
		{fairPairKey("a", "b"): pairEvidence(
			pairCandidate("a", ""), pairCandidate("b", "https://b/"), 1, 0, 0,
		)},
		{fairPairKey("a", "b"): pairEvidence(
			pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 0, 0, 0,
		)},
		{fairPairKey("a", "b"): pairEvidence(
			pairCandidate("a", "https://a/"), pairCandidate("b", "https://b/"), 1, 2, 0,
		)},
		{fairPairKey("a", "b"): pairEvidence(
			pairCandidate(strings.Repeat("a", maximumClusterIdentityBytes+1), "https://a/"),
			pairCandidate("b", "https://b/"), 1, 0, 0,
		)},
	}
	for index, pairs := range invalid {
		if err := validateFairPairEvidence(pairs); err == nil {
			t.Fatalf("invalid fair pairs %d accepted", index)
		}
	}
}
