package clickcapture

import (
	"context"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

const (
	gradeRelevant       = 1
	gradeHighlyRelevant = 2
	dominanceFraction   = 0.5
)

type resultEstimate struct {
	url    string
	weight float64
	score  float64
}

func (s *Store) ImplicitJudgments(
	ctx context.Context,
	minimumImpressions int,
) ([]searcheval.Judgment, error) {
	aggregates, err := s.Aggregates(ctx)
	if err != nil {
		return nil, err
	}

	return DeriveJudgments(aggregates, minimumImpressions), nil
}

func DeriveJudgments(
	aggregates []QueryEvidence,
	minimumImpressions int,
) []searcheval.Judgment {
	minimumImpressions = max(1, minimumImpressions)
	judgments := make([]searcheval.Judgment, 0, len(aggregates))
	for _, aggregate := range aggregates {
		judgment, present := derivedJudgment(aggregate, minimumImpressions)
		if present {
			judgments = append(judgments, judgment)
		}
	}
	sort.Slice(judgments, func(left, right int) bool {
		return judgments[left].Query < judgments[right].Query
	})

	return judgments
}

func derivedJudgment(
	aggregate QueryEvidence,
	minimumImpressions int,
) (searcheval.Judgment, bool) {
	estimates := queryEstimates(aggregate, minimumImpressions)
	top := highestEstimateScore(estimates)
	if top <= 0 {
		return searcheval.Judgment{}, false
	}
	judgment := searcheval.Judgment{
		Query:    aggregate.Query,
		Relevant: gradedEstimateURLs(estimates, top),
	}
	if aggregate.ObservedAtUnix > 0 {
		judgment.ObservedAt = time.Unix(aggregate.ObservedAtUnix, 0).UTC()
	}

	return judgment, true
}

func highestEstimateScore(estimates []resultEstimate) float64 {
	top := 0.0
	for _, estimate := range estimates {
		top = max(top, estimate.score)
	}

	return top
}

func gradedEstimateURLs(estimates []resultEstimate, top float64) map[string]int {
	relevant := make(map[string]int, len(estimates))
	for _, estimate := range estimates {
		if estimate.score <= 0 {
			continue
		}
		grade := gradeRelevant
		if estimate.score >= dominanceFraction*top {
			grade = gradeHighlyRelevant
		}
		relevant[estimate.url] = grade
	}

	return relevant
}

func queryEstimates(
	aggregate QueryEvidence,
	minimumImpressions int,
) []resultEstimate {
	byCluster := map[string]resultEstimate{}
	for _, model := range aggregate.Models {
		if model.RandomizedImpressions < minimumImpressions {
			continue
		}
		for cluster, result := range model.Results {
			if result.RandomizedImpressions < minimumImpressions ||
				result.ClippedExposureWeight <= 0 {
				continue
			}
			ips := min(
				result.ClippedClickWeight/float64(result.RandomizedImpressions),
				1,
			)
			snips := min(result.ClippedClickWeight/result.ClippedExposureWeight, 1)
			score := 0.0
			if ips > 0 && snips > 0 {
				score = 2 * ips * snips / (ips + snips)
			}
			estimate := byCluster[cluster]
			estimate.url = representativeURL(estimate.url, result.URLIdentity)
			estimate.score += score * float64(result.RandomizedImpressions)
			estimate.weight += float64(result.RandomizedImpressions)
			byCluster[cluster] = estimate
		}
	}
	estimates := make([]resultEstimate, 0, len(byCluster))
	for _, estimate := range byCluster {
		if estimate.weight > 0 {
			estimate.score /= estimate.weight
			estimates = append(estimates, estimate)
		}
	}
	sort.Slice(estimates, func(left, right int) bool {
		return estimates[left].url < estimates[right].url
	})

	return estimates
}
