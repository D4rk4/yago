package searcheval

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type fakeSearcher struct {
	byQuery map[string][]searchcore.Result
	err     error
}

func (f fakeSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if f.err != nil {
		return searchcore.Response{}, f.err
	}

	return searchcore.Response{Results: f.byQuery[req.Query]}, nil
}

func results(urls ...string) []searchcore.Result {
	out := make([]searchcore.Result, 0, len(urls))
	for _, url := range urls {
		out = append(out, searchcore.Result{URL: url})
	}

	return out
}

func TestNDCG(t *testing.T) {
	relevant := map[string]int{"a": 1, "c": 1}

	// Ideal order scores 1.0.
	if got := NDCG(results("a", "c", "b"), relevant, 3); math.Abs(got-1) > 1e-9 {
		t.Fatalf("ideal NDCG = %v, want 1", got)
	}
	// A relevant document demoted to rank 3 loses gain.
	got := NDCG(results("a", "b", "c"), relevant, 3)
	want := 1.5 / (1.0 + 1.0/math.Log2(3))
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("NDCG = %v, want %v", got, want)
	}
	// No judged document relevant → 0.
	if got := NDCG(results("x", "y"), relevant, 3); got != 0 {
		t.Fatalf("no-hit NDCG = %v, want 0", got)
	}
	// Empty relevance set (idcg 0) → 0.
	if got := NDCG(results("a"), map[string]int{}, 3); got != 0 {
		t.Fatalf("no-judgments NDCG = %v, want 0", got)
	}
	// Non-positive k → 0.
	if got := NDCG(results("a"), relevant, 0); got != 0 {
		t.Fatalf("k=0 NDCG = %v, want 0", got)
	}
	// Graded relevance: a highly-relevant doc first beats it lower.
	graded := map[string]int{"a": 3, "b": 1}
	high := NDCG(results("a", "b"), graded, 2)
	low := NDCG(results("b", "a"), graded, 2)
	if !(high > low) || math.Abs(high-1) > 1e-9 {
		t.Fatalf("graded NDCG high=%v low=%v", high, low)
	}
}

func TestEvaluate(t *testing.T) {
	searcher := fakeSearcher{byQuery: map[string][]searchcore.Result{
		"perfect": results("a", "b"),
		"bad":     results("b", "a"),
	}}
	judgments := []Judgment{
		{Query: "perfect", Relevant: map[string]int{"a": 1}},
		{Query: "bad", Relevant: map[string]int{"a": 1}},
	}
	report, err := Evaluate(context.Background(), searcher, judgments, 10)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.K != 10 || len(report.PerQuery) != 2 {
		t.Fatalf("report = %+v", report)
	}
	if math.Abs(report.PerQuery["perfect"]-1) > 1e-9 {
		t.Fatalf("perfect query NDCG = %v", report.PerQuery["perfect"])
	}
	bad := report.PerQuery["bad"]
	if !(bad > 0 && bad < 1) {
		t.Fatalf("bad query NDCG = %v, want between 0 and 1", bad)
	}
	if math.Abs(report.Mean-(1+bad)/2) > 1e-9 {
		t.Fatalf("mean = %v", report.Mean)
	}

	// No judgments → zero mean, no division by zero.
	empty, err := Evaluate(context.Background(), searcher, nil, 10)
	if err != nil || empty.Mean != 0 {
		t.Fatalf("empty evaluate = %+v err=%v", empty, err)
	}

	// A searcher error surfaces.
	if _, err := Evaluate(
		context.Background(),
		fakeSearcher{err: errors.New("index down")},
		judgments,
		10,
	); err == nil {
		t.Fatal("expected searcher error to surface")
	}
}

func TestPseudoJudgments(t *testing.T) {
	labels := []Label{
		{Query: "montenegro", URL: "https://a/1"},
		{Query: "montenegro", URL: "https://a/2", Grade: 2},
		{Query: "", URL: "https://a/3"}, // skipped: no query
		{Query: "empty", URL: ""},       // skipped: no url
		{Query: "graded", URL: "https://a/4", Grade: 5},
	}
	judgments := PseudoJudgments(labels)
	if len(judgments) != 2 {
		t.Fatalf("judgments = %+v", judgments)
	}
	// Grouping preserves first-seen order and default/explicit grades.
	if judgments[0].Query != "montenegro" ||
		judgments[0].Relevant["https://a/1"] != 1 ||
		judgments[0].Relevant["https://a/2"] != 2 {
		t.Fatalf("montenegro judgment = %+v", judgments[0])
	}
	if judgments[1].Query != "graded" || judgments[1].Relevant["https://a/4"] != 5 {
		t.Fatalf("graded judgment = %+v", judgments[1])
	}
}
