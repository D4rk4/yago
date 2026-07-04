package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type stubSearcher struct {
	response searchcore.Response
	err      error
}

func (s stubSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	return s.response, s.err
}

func TestWithSearchMetricsNilCollectorReturnsNext(t *testing.T) {
	if _, wrapped := withSearchMetrics(stubSearcher{}, nil).(searchMetricsSearcher); wrapped {
		t.Fatal("a nil collector must not wrap the searcher")
	}
}

func TestWithSearchMetricsPassesResponseAndErrorThrough(t *testing.T) {
	collector := metrics.NewSearchMetrics(prometheus.NewRegistry())
	want := errors.New("boom")
	next := stubSearcher{
		response: searchcore.Response{
			TotalResults:    7,
			PartialFailures: []searchcore.PartialFailure{{Source: "remote"}},
		},
		err: want,
	}

	got, err := withSearchMetrics(
		next,
		collector,
	).Search(context.Background(), searchcore.Request{})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if got.TotalResults != 7 {
		t.Fatalf("results = %d, want 7 passed through", got.TotalResults)
	}
}
