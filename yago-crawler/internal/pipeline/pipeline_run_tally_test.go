package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
)

func TestPipelineRunTallyCountsSuccessfulPage(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)

	tally := runOneJob(t, p, frontier).outcome

	if tally.Fetched != 1 || tally.Indexed != 1 || tally.Failed != 0 {
		t.Fatalf("tally = %#v, want fetched 1 indexed 1 failed 0", tally)
	}
}

func TestPipelineRunTallyCountsHardFailure(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)

	tally := runOneJob(t, p, frontier).outcome

	if tally.Failed != 1 || tally.Fetched != 0 || tally.Indexed != 0 {
		t.Fatalf("tally = %#v, want failed 1", tally)
	}
}

// Regression: a rejected fetch (blocked target, bad status, wrong content
// type) used to increment no counter at all, so a run finished with every
// number at zero and no trace of why (seen live with a bogus "::" DNS
// record). A non-robots reject now counts as a failed fetch.
func TestPipelineRunTallyCountsRejectedFetchAsFailed(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)

	tally := runOneJob(t, p, frontier).outcome

	if tally.Failed != 1 || tally.Fetched != 0 || tally.Indexed != 0 ||
		tally.RobotsDenied != 0 {
		t.Fatalf("tally = %#v, want the rejected fetch counted as failed", tally)
	}
}

func TestPipelineRunTallyCountsRobotsDenial(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf(
				"robots disallow: %w: %w", robots.ErrDisallowed, pagefetch.ErrPageRejected,
			)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)

	tally := runOneJob(t, p, frontier).outcome

	if tally.RobotsDenied != 1 || tally.Failed != 0 || tally.Fetched != 0 {
		t.Fatalf("tally = %#v, want robotsDenied 1 and no failure", tally)
	}
}
