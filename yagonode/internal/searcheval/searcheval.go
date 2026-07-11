// Package searcheval scores a searcher's ranking quality with NDCG@k so a test
// can gate ranking changes against regression, and turns query→relevant-URL
// labels (including corpus-derived pseudo-labels) into judgments.
package searcheval

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const maximumRelevanceGrade = 30

// Judgment is one query paired with the graded relevance of documents by URL: a
// grade of 0 (or an absent URL) is non-relevant, higher grades are more
// relevant.
type Judgment struct {
	Query          string
	QueryCluster   string
	ObservedAt     time.Time
	Relevant       map[string]int
	ClusterIntents map[string][]string
	Navigational   bool
	SliceNames     []string
}

// Report summarizes an evaluation run: the NDCG@k of each query and their mean.
type Report struct {
	K        int
	Mean     float64
	PerQuery map[string]float64
}

// NDCG is the normalized discounted cumulative gain of the results at rank k,
// grading each result by its judged relevance (0 when unjudged) with an
// exponential gain and a logarithmic rank discount. It is 0 when no judged
// document is relevant, and 1 when the results are in ideal order.
func NDCG(results []searchcore.Result, relevant map[string]int, k int) float64 {
	if k <= 0 {
		return 0
	}
	ideal := idealGains(relevant)
	idcg := discountedSum(ideal, k)
	if idcg == 0 {
		return 0
	}
	actual := make([]int, 0, len(results))
	for _, result := range results {
		actual = append(actual, relevant[result.URL])
	}

	return discountedSum(actual, k) / idcg
}

// discountedSum is the DCG of the first k grades: sum of (2^grade - 1) divided
// by log2(rank + 1), rank being 1-based.
func discountedSum(grades []int, k int) float64 {
	sum := 0.0
	for rank := 1; rank <= k && rank <= len(grades); rank++ {
		gain := math.Exp2(float64(boundedGrade(grades[rank-1]))) - 1
		sum += gain / math.Log2(float64(rank)+1)
	}

	return sum
}

// idealGains is the relevance grades in descending order — the best ranking a
// perfect searcher could produce.
func idealGains(relevant map[string]int) []int {
	grades := make([]int, 0, len(relevant))
	for _, grade := range relevant {
		grades = append(grades, boundedGrade(grade))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))

	return grades
}

// Evaluate runs each judgment's query through the searcher and reports NDCG@k
// per query and the mean across all judgments.
func Evaluate(
	ctx context.Context,
	searcher searchcore.Searcher,
	judgments []Judgment,
	k int,
) (Report, error) {
	report := Report{K: k, PerQuery: make(map[string]float64, len(judgments))}
	total := 0.0
	for _, judgment := range judgments {
		request := searchcore.RequestWithParsedQuery(searchcore.Request{
			Query: judgment.Query,
			Limit: k,
		})
		response, err := searcher.Search(ctx, request)
		if err != nil {
			return Report{}, fmt.Errorf("evaluate query %q: %w", judgment.Query, err)
		}
		score := NDCG(response.Results, judgment.Relevant, k)
		report.PerQuery[judgment.Query] = score
		total += score
	}
	if len(judgments) > 0 {
		report.Mean = total / float64(len(judgments))
	}

	return report, nil
}

// Label is a single query→relevant-URL pair, the raw material for judgments and
// for corpus-derived pseudo-labels (query taken from a document's own title,
// relevant document being that page).
type Label struct {
	Query string
	URL   string
	Grade int
}

// PseudoJudgments groups labels by query into judgments, so a node can build a
// self-evaluation set from its own corpus without human relevance assessments.
// A label with a non-positive grade defaults to grade 1 (relevant).
func PseudoJudgments(labels []Label) []Judgment {
	order := make([]string, 0)
	byQuery := make(map[string]map[string]int)
	for _, label := range labels {
		if label.Query == "" || label.URL == "" {
			continue
		}
		grade := label.Grade
		if grade <= 0 {
			grade = 1
		}
		grade = boundedGrade(grade)
		if _, ok := byQuery[label.Query]; !ok {
			byQuery[label.Query] = map[string]int{}
			order = append(order, label.Query)
		}
		byQuery[label.Query][label.URL] = grade
	}
	judgments := make([]Judgment, 0, len(order))
	for _, query := range order {
		judgments = append(judgments, Judgment{Query: query, Relevant: byQuery[query]})
	}

	return judgments
}

func boundedGrade(grade int) int {
	return min(max(grade, 0), maximumRelevanceGrade)
}
