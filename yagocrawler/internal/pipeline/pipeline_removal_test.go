package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagomodel"
)

type spyEmitter struct {
	mu         sync.Mutex
	emits      int
	removals   []string
	removalErr error
}

func (e *spyEmitter) Emit(
	context.Context,
	yagocrawlcontract.DocumentIngest,
	[]yagomodel.RWIPosting,
	yagomodel.URIMetadataRow,
	ingest.Envelope,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emits++

	return nil
}

func (e *spyEmitter) EmitRemoval(_ context.Context, sourceURL string, _ []byte, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removals = append(e.removals, sourceURL)

	return e.removalErr
}

func (e *spyEmitter) removalURLs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.removals...)
}

func (e *spyEmitter) emitCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.emits
}

func TestPipelineTombstonesGonePage(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: http.StatusNotFound}
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	done := runOneJob(t, p, frontier)
	if done.failed {
		t.Error("a successful tombstone emit must not mark the delivery failed")
	}
	if urls := emitter.removalURLs(); len(urls) != 1 || urls[0] != "https://example.com/" {
		t.Fatalf("removals = %v, want the job URL", urls)
	}
	if emitter.emitCount() != 0 {
		t.Errorf("a gone page must not emit a document, emits = %d", emitter.emitCount())
	}
}

func TestPipelineMarksDeliveryFailedOnRemovalEmitError(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{removalErr: errors.New("emit failed")}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, &pagefetch.GoneError{Status: http.StatusGone}
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	if done := runOneJob(t, p, frontier); !done.failed {
		t.Error("a removal emit failure must mark the delivery failed so the order redelivers")
	}
}

func TestPipelineDoesNotTombstoneOnGenericRejection(t *testing.T) {
	frontier := newRecordingFrontier()
	emitter := &spyEmitter{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("status 500: %w", pagefetch.ErrPageRejected)
		}),
		pageindex.NewIndexBuilder(),
		emitter,
	)
	done := runOneJob(t, p, frontier)
	if done.failed {
		t.Error("a generic rejection must not mark the delivery failed")
	}
	if urls := emitter.removalURLs(); len(urls) != 0 {
		t.Fatalf("a non-gone rejection must not emit a removal, got %v", urls)
	}
}
