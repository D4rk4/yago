package searcheval

import (
	"math"
	"sort"
	"strings"
	"time"
)

type CanonicalJudgment struct {
	Query            string
	QueryCluster     string
	ObservedAt       time.Time
	RelevantClusters map[string]int
	ClusterIntents   map[string][]string
	Navigational     bool
	SliceNames       []string
}

type RankedCandidate struct {
	URL               string
	CanonicalCluster  string
	RegistrableDomain string
	Score             float64
	Unsafe            bool
	Spam              bool
}

type QueryObservation struct {
	ID                    string
	Judgment              CanonicalJudgment
	Candidates            []RankedCandidate
	PeerResourcesMeasured bool
	PeerBytes             int64
	PeerTimeouts          int
	RerankLatency         time.Duration
}

func PoolCandidates(rankings ...[]RankedCandidate) []RankedCandidate {
	pooled := make(map[string]RankedCandidate)
	for _, ranking := range rankings {
		for _, candidate := range ranking {
			cluster := candidateCluster(candidate)
			if cluster == "" {
				continue
			}
			candidate.CanonicalCluster = cluster
			current, found := pooled[cluster]
			if !found || candidatePrecedes(candidate, current) {
				candidate.Unsafe = candidate.Unsafe || current.Unsafe
				candidate.Spam = candidate.Spam || current.Spam
				pooled[cluster] = candidate
			} else {
				current.Unsafe = current.Unsafe || candidate.Unsafe
				current.Spam = current.Spam || candidate.Spam
				pooled[cluster] = current
			}
		}
	}
	out := make([]RankedCandidate, 0, len(pooled))
	for _, candidate := range pooled {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if rankedCandidateScore(out[i].Score) != rankedCandidateScore(out[j].Score) {
			return rankedCandidateScore(out[i].Score) > rankedCandidateScore(out[j].Score)
		}

		return out[i].CanonicalCluster < out[j].CanonicalCluster
	})

	return out
}

func candidateCluster(candidate RankedCandidate) string {
	if cluster := strings.TrimSpace(candidate.CanonicalCluster); cluster != "" {
		return cluster
	}

	return strings.TrimSpace(candidate.URL)
}

func candidatePrecedes(candidate, current RankedCandidate) bool {
	candidateScore := rankedCandidateScore(candidate.Score)
	currentScore := rankedCandidateScore(current.Score)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}

	return candidate.URL < current.URL
}

func rankedCandidateScore(score float64) float64 {
	if math.IsNaN(score) {
		return math.Inf(-1)
	}

	return score
}

func judgmentCluster(judgment CanonicalJudgment) string {
	cluster := strings.Join(strings.Fields(strings.ToLower(judgment.QueryCluster)), " ")
	if cluster != "" {
		return cluster
	}

	return strings.Join(strings.Fields(strings.ToLower(judgment.Query)), " ")
}

func observationID(observation QueryObservation) string {
	if id := strings.TrimSpace(observation.ID); id != "" {
		return id
	}

	return judgmentCluster(observation.Judgment)
}

func candidateGrade(judgment CanonicalJudgment, candidate RankedCandidate) int {
	return boundedGrade(judgment.RelevantClusters[candidateCluster(candidate)])
}
