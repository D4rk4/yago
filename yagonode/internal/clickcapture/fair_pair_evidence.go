package clickcapture

import (
	"fmt"
	"math"
	"sort"
)

const fairPairConfidenceZ = 1.96

type FairPairEvidence struct {
	FirstCluster  string `json:"first_cluster"`
	FirstURL      string `json:"first_url"`
	SecondCluster string `json:"second_cluster"`
	SecondURL     string `json:"second_url"`
	Impressions   int    `json:"impressions"`
	FirstClicks   int    `json:"first_clicks"`
	SecondClicks  int    `json:"second_clicks"`
}

type FairPairMember struct {
	FirstCluster  string
	FirstURL      string
	SecondCluster string
	SecondURL     string
}

func addFairPairImpressions(model *ModelEvidence, candidates []DisplayedCandidate) {
	for _, pair := range displayedFairPairs(candidates) {
		if model.FairPairs == nil {
			model.FairPairs = map[string]FairPairEvidence{}
		}
		key := fairPairKey(pair.FirstCluster, pair.SecondCluster)
		evidence := model.FairPairs[key]
		evidence.FirstCluster = pair.FirstCluster
		evidence.FirstURL = representativeURL(evidence.FirstURL, pair.FirstURL)
		evidence.SecondCluster = pair.SecondCluster
		evidence.SecondURL = representativeURL(evidence.SecondURL, pair.SecondURL)
		evidence.Impressions = incrementAggregate(evidence.Impressions)
		model.FairPairs[key] = evidence
	}
}

func addFairPairClick(model *ModelEvidence, clicked string, pair FairPairMember) {
	key := fairPairKey(pair.FirstCluster, pair.SecondCluster)
	evidence, exists := model.FairPairs[key]
	if !exists {
		return
	}
	switch {
	case clicked == evidence.FirstCluster && evidence.FirstClicks < evidence.Impressions:
		evidence.FirstClicks = incrementAggregate(evidence.FirstClicks)
	case clicked == evidence.SecondCluster && evidence.SecondClicks < evidence.Impressions:
		evidence.SecondClicks = incrementAggregate(evidence.SecondClicks)
	default:
		return
	}
	model.FairPairs[key] = evidence
}

func displayedFairPairs(candidates []DisplayedCandidate) []FairPairMember {
	ordered := append([]DisplayedCandidate(nil), candidates...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Position < ordered[right].Position
	})
	pairs := make([]FairPairMember, 0, len(ordered)/2)
	for index := 0; index+1 < len(ordered); index++ {
		left := ordered[index]
		right := ordered[index+1]
		if left.Propensity != 0.5 || right.Propensity != 0.5 ||
			right.Position != left.Position+1 ||
			absInt(right.OriginalIndex-left.OriginalIndex) != 1 {
			continue
		}
		pairs = append(pairs, canonicalFairPair(left.Candidate, right.Candidate))
		index++
	}

	return pairs
}

func pairedFairPairMember(
	candidates []DisplayedCandidate,
	clicked DisplayedCandidate,
) *FairPairMember {
	if clicked.Propensity != 0.5 {
		return nil
	}
	for _, candidate := range candidates {
		if candidate.URLIdentity == clicked.URLIdentity || candidate.Propensity != 0.5 ||
			absInt(candidate.Position-clicked.Position) != 1 ||
			absInt(candidate.OriginalIndex-clicked.OriginalIndex) != 1 {
			continue
		}
		pair := canonicalFairPair(clicked.Candidate, candidate.Candidate)

		return &pair
	}

	return nil
}

func canonicalFairPair(first, second Candidate) FairPairMember {
	if second.ClusterIdentity < first.ClusterIdentity {
		first, second = second, first
	}

	return FairPairMember{
		FirstCluster:  first.ClusterIdentity,
		FirstURL:      first.URLIdentity,
		SecondCluster: second.ClusterIdentity,
		SecondURL:     second.URLIdentity,
	}
}

func fairPairKey(first, second string) string {
	return first + "\x00" + second
}

func confidentFairPairWinner(
	pair FairPairEvidence,
	minimumImpressions int,
) (string, string, float64, bool) {
	clicks := pair.FirstClicks + pair.SecondClicks
	if pair.Impressions < minimumImpressions || clicks < minimumImpressions {
		return "", "", 0, false
	}
	share := float64(pair.FirstClicks) / float64(clicks)
	lower, upper := wilsonInterval(share, clicks)
	if lower > 0.5 {
		return pair.FirstCluster, pair.FirstURL, lower - 0.5, true
	}
	if upper < 0.5 {
		return pair.SecondCluster, pair.SecondURL, 0.5 - upper, true
	}

	return "", "", 0, false
}

func wilsonInterval(share float64, samples int) (float64, float64) {
	count := float64(samples)
	zSquared := fairPairConfidenceZ * fairPairConfidenceZ
	center := (share + zSquared/(2*count)) / (1 + zSquared/count)
	width := fairPairConfidenceZ * math.Sqrt(
		share*(1-share)/count+zSquared/(4*count*count),
	) / (1 + zSquared/count)

	return max(0, center-width), min(1, center+width)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

func validateFairPairEvidence(pairs map[string]FairPairEvidence) error {
	if len(pairs) > maximumResultsPerModel {
		return fmt.Errorf("fair-pair evidence exceeds its bound")
	}
	for key, pair := range pairs {
		if pair.FirstCluster == "" || pair.SecondCluster == "" ||
			pair.FirstCluster >= pair.SecondCluster ||
			len(pair.FirstCluster) > maximumClusterIdentityBytes ||
			len(pair.SecondCluster) > maximumClusterIdentityBytes ||
			pair.FirstURL == "" || pair.SecondURL == "" ||
			len(pair.FirstURL) > maximumURLIdentityBytes ||
			len(pair.SecondURL) > maximumURLIdentityBytes ||
			key != fairPairKey(pair.FirstCluster, pair.SecondCluster) {
			return fmt.Errorf("fair-pair identity is invalid")
		}
		if !boundedAggregate(pair.Impressions) || pair.Impressions == 0 ||
			!boundedAggregate(pair.FirstClicks) || !boundedAggregate(pair.SecondClicks) ||
			pair.FirstClicks > pair.Impressions || pair.SecondClicks > pair.Impressions {
			return fmt.Errorf("fair-pair aggregate is invalid")
		}
	}

	return nil
}
