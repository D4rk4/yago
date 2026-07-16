package crawlresults

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type searchIndexWriteScript struct {
	writeErr  error
	batchRuns int
	documents int
}

func (s *searchIndexWriteScript) IndexBatch(
	_ context.Context,
	documents []documentstore.Document,
) error {
	s.batchRuns++
	s.documents += len(documents)

	return s.writeErr
}

func (*searchIndexWriteScript) Index(context.Context, documentstore.Document) error {
	return nil
}

func (*searchIndexWriteScript) Delete(context.Context, string) error {
	return nil
}

func (*searchIndexWriteScript) Search(
	context.Context,
	searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func (*searchIndexWriteScript) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

type searchIndexWriteObservation struct {
	duration  time.Duration
	documents int
	failed    bool
}

type recordingSearchIndexWriteObserver struct {
	observations []searchIndexWriteObservation
}

func (o *recordingSearchIndexWriteObserver) ObserveSearchIndexWrite(
	duration time.Duration,
	documents int,
	failed bool,
) {
	o.observations = append(o.observations, searchIndexWriteObservation{
		duration:  duration,
		documents: documents,
		failed:    failed,
	})
}

func TestIndexDocumentsObservesOneBatchAttempt(t *testing.T) {
	t.Parallel()

	writeFailure := errors.New("write failed")
	for _, test := range []struct {
		name      string
		writeErr  error
		wantError bool
	}{
		{name: "success"},
		{name: "failure", writeErr: writeFailure, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			index := &searchIndexWriteScript{writeErr: test.writeErr}
			observer := &recordingSearchIndexWriteObserver{}
			consumer := NewIngestConsumerWithIndex(nil, nil, index, nil, nil)
			consumer.ObserveSearchIndexWrites(observer)
			consumer.ObserveSearchIndexWrites(nil)
			documents := make([]documentstore.Document, 3)

			err := consumer.indexDocuments(t.Context(), documents)
			if errors.Is(err, writeFailure) != test.wantError {
				t.Fatalf("write error = %v, want failure %v", err, test.wantError)
			}
			if index.batchRuns != 1 || index.documents != len(documents) {
				t.Fatalf(
					"batch runs = %d documents = %d",
					index.batchRuns,
					index.documents,
				)
			}
			if len(observer.observations) != 1 {
				t.Fatalf("observations = %d, want 1", len(observer.observations))
			}
			observation := observer.observations[0]
			if observation.duration < 0 ||
				observation.documents != len(documents) ||
				observation.failed != test.wantError {
				t.Fatalf("observation = %+v", observation)
			}
		})
	}
}
