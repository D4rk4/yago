package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
)

type refetchCountingObserver struct {
	countingPipelineObserver
	notModifiedRefetched int
}

func (o *refetchCountingObserver) NotModifiedRefetched() { o.notModifiedRefetched++ }

func TestPipelineObservesRefetchOnlyWhenNodeDoesNotRecordNotModified(t *testing.T) {
	for _, tc := range []struct {
		name              string
		notModifiedResult func(context.Context) error
		wantRefetched     int
	}{
		{
			name: "unrecorded",
			notModifiedResult: func(context.Context) error {
				return fmt.Errorf("publish: %w", ingest.ErrNotModifiedUnrecorded)
			},
			wantRefetched: 1,
		},
		{name: "recorded"},
		{
			name: "delivery failed",
			notModifiedResult: func(context.Context) error {
				return errors.New("node unavailable")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier := newRecordingFrontier()
			source := &validatorAwareSource{respond: notModifiedWhenConditional}
			emitter := &recrawlEmitter{notModifiedResult: tc.notModifiedResult}
			observer := &refetchCountingObserver{}
			p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter,
				pipeline.WithNotModifiedEmitter(emitter), pipeline.WithObserver(observer))

			runJob(t, p, frontier, conditionalRecrawlJob())

			if observer.notModifiedRefetched != tc.wantRefetched {
				t.Fatalf("not-modified refetches = %d, want %d",
					observer.notModifiedRefetched, tc.wantRefetched)
			}
		})
	}
}
