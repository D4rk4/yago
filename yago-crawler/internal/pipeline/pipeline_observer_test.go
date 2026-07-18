package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type countingPipelineObserver struct {
	jobStarted      int
	jobFinished     int
	fetchAttempted  int
	fetchSucceeded  int
	fetchFailed     int
	ingestPublished int
	bytes           int
}

func (c *countingPipelineObserver) JobStarted()      { c.jobStarted++ }
func (c *countingPipelineObserver) JobFinished()     { c.jobFinished++ }
func (c *countingPipelineObserver) FetchAttempted()  { c.fetchAttempted++ }
func (c *countingPipelineObserver) FetchFailed()     { c.fetchFailed++ }
func (c *countingPipelineObserver) IngestPublished() { c.ingestPublished++ }

func (c *countingPipelineObserver) FetchSucceeded(bytes int) {
	c.fetchSucceeded++
	c.bytes += bytes
}

func okEmitter() ingest.BatchEmitter {
	return emitFunc(func(
		context.Context,
		yagocrawlcontract.DocumentIngest,
		[]yagomodel.RWIPosting,
		yagomodel.URIMetadataRow,
		ingest.Envelope,
	) error {
		return nil
	})
}

func TestPipelineObserverRecordsSuccessfulFetch(t *testing.T) {
	frontier := newRecordingFrontier()
	observer := &countingPipelineObserver{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithObserver(observer),
	)

	runOneJob(t, p, frontier)

	if observer.jobStarted != 1 || observer.jobFinished != 1 || observer.fetchAttempted != 1 ||
		observer.fetchSucceeded != 1 || observer.fetchFailed != 0 ||
		observer.ingestPublished != 1 || observer.bytes == 0 {
		t.Fatalf("observer = %#v", observer)
	}
}

func TestPipelineObserverRecordsHardFailure(t *testing.T) {
	frontier := newRecordingFrontier()
	observer := &countingPipelineObserver{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithObserver(observer),
	)

	runOneJob(t, p, frontier)

	if observer.fetchAttempted != 1 || observer.fetchFailed != 1 ||
		observer.fetchSucceeded != 0 || observer.ingestPublished != 0 {
		t.Fatalf("observer = %#v", observer)
	}
}

func TestPipelineObserverCountsRejectedFetchAsFailed(t *testing.T) {
	frontier := newRecordingFrontier()
	observer := &countingPipelineObserver{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithObserver(observer),
	)

	runOneJob(t, p, frontier)

	if observer.fetchAttempted != 1 || observer.fetchFailed != 1 {
		t.Fatalf("observer = %#v, want the rejected fetch counted as failed", observer)
	}
}

func TestPipelineNilObserverStaysNoop(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
		pipeline.WithObserver(nil),
	)

	runOneJob(t, p, frontier)
}
