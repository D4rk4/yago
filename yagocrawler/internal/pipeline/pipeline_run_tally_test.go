package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
)

type countingRunTally struct {
	fetched      int
	indexed      int
	failed       int
	robotsDenied int
}

func (c *countingRunTally) Fetched([]byte) { c.fetched++ }

func (c *countingRunTally) Indexed([]byte) { c.indexed++ }

func (c *countingRunTally) Failed([]byte) { c.failed++ }

func (c *countingRunTally) RobotsDenied([]byte) { c.robotsDenied++ }

func TestPipelineRunTallyCountsSuccessfulPage(t *testing.T) {
	frontier := newRecordingFrontier()
	tally := &countingRunTally{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithRunTally(tally),
	)

	runOneJob(t, p, frontier)

	if tally.fetched != 1 || tally.indexed != 1 || tally.failed != 0 {
		t.Fatalf("tally = %#v, want fetched 1 indexed 1 failed 0", tally)
	}
}

func TestPipelineRunTallyCountsHardFailure(t *testing.T) {
	frontier := newRecordingFrontier()
	tally := &countingRunTally{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithRunTally(tally),
	)

	runOneJob(t, p, frontier)

	if tally.failed != 1 || tally.fetched != 0 || tally.indexed != 0 {
		t.Fatalf("tally = %#v, want failed 1", tally)
	}
}

// Regression: a rejected fetch (blocked target, bad status, wrong content
// type) used to increment no counter at all, so a run finished with every
// number at zero and no trace of why (seen live with a bogus "::" DNS
// record). A non-robots reject now counts as a failed fetch.
func TestPipelineRunTallyCountsRejectedFetchAsFailed(t *testing.T) {
	frontier := newRecordingFrontier()
	tally := &countingRunTally{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithRunTally(tally),
	)

	runOneJob(t, p, frontier)

	if tally.failed != 1 || tally.fetched != 0 || tally.indexed != 0 ||
		tally.robotsDenied != 0 {
		t.Fatalf("tally = %#v, want the rejected fetch counted as failed", tally)
	}
}

func TestPipelineRunTallyCountsRobotsDenial(t *testing.T) {
	frontier := newRecordingFrontier()
	tally := &countingRunTally{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf(
				"robots disallow: %w: %w", robots.ErrDisallowed, pagefetch.ErrPageRejected,
			)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithRunTally(tally),
	)

	runOneJob(t, p, frontier)

	if tally.robotsDenied != 1 || tally.failed != 0 || tally.fetched != 0 {
		t.Fatalf("tally = %#v, want robotsDenied 1 and no failure", tally)
	}
}
