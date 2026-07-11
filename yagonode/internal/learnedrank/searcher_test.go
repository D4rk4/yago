package learnedrank

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type searcherFixture struct {
	response searchcore.Response
	err      error
	request  *searchcore.Request
}

func (s searcherFixture) Search(
	_ context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	if s.request != nil {
		*s.request = request
	}

	return s.response, s.err
}

func TestSearcherAppliesActiveModelAndPreservesResponse(t *testing.T) {
	ranker, err := NewRanker(3)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"serving",
		mustLinearModel(t, linearWeights(map[int]float64{0: 1})),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	request := searchcore.Request{Query: "query", Limit: 2, Offset: 1}
	var innerRequest searchcore.Request
	response, err := NewSearcher(
		searcherFixture{request: &innerRequest, response: searchcore.Response{
			TotalResults: 3,
			Results: []searchcore.Result{
				rankingResult("low", 1, 1),
				rankingResult("high", 3, 2),
				rankingResult("middle", 2, 3),
			},
			PartialFailures: []searchcore.PartialFailure{{Source: "peer", Reason: "timeout"}},
		}},
		ranker,
	).Search(t.Context(), request)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := resultURLs(
		response.Results,
	); !reflect.DeepEqual(
		got,
		[]string{"high", "middle", "low"},
	) {
		t.Fatalf("results = %v", got)
	}
	if response.TotalResults != 3 || !reflect.DeepEqual(response.Request, request) ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("response metadata = %#v", response)
	}
	if innerRequest.Offset != 0 || innerRequest.Limit != ranker.CandidateWindow() {
		t.Fatalf("inner request = %#v", innerRequest)
	}
	var deepRequest searchcore.Request
	if _, err := NewSearcher(
		searcherFixture{request: &deepRequest},
		ranker,
	).Search(t.Context(), searchcore.Request{Query: "query", Limit: 4, Offset: 2}); err != nil {
		t.Fatal(err)
	}
	if deepRequest.Offset != 0 || deepRequest.Limit != 6 {
		t.Fatalf("deep inner request = %#v", deepRequest)
	}
}

func TestSearcherNoRankerAndFailures(t *testing.T) {
	inner := searcherFixture{
		response: searchcore.Response{Results: []searchcore.Result{{URL: "a"}}},
	}
	if NewSearcher(&inner, nil) != &inner {
		t.Fatal("nil ranker did not preserve the inner searcher")
	}
	ranker, err := NewRanker(2)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	var inactiveRequest searchcore.Request
	requested := searchcore.Request{Query: "inactive", Offset: 2}
	if _, err := NewSearcher(
		searcherFixture{request: &inactiveRequest},
		ranker,
	).Search(t.Context(), requested); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inactiveRequest, requested) {
		t.Fatalf("inactive inner request = %#v", inactiveRequest)
	}
	if _, err := NewSearcher(
		searcherFixture{err: errors.New("backend down")},
		ranker,
	).Search(t.Context(), searchcore.Request{}); err == nil {
		t.Fatal("inner search failure was not returned")
	}
	if err := ranker.Activate(mustSnapshot(
		t,
		"invalid-evidence",
		mustLinearModel(t, linearWeights(nil)),
	)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	invalid := searcherFixture{response: searchcore.Response{Results: []searchcore.Result{
		rankingResult("a", 1e308, 1),
		rankingResult("b", 1, 2),
	}}}
	if _, err := NewSearcher(invalid, ranker).Search(
		t.Context(),
		searchcore.Request{},
	); err == nil {
		t.Fatal("ranking failure was not returned")
	}
}
