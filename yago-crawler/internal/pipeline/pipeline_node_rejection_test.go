package pipeline_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// immediateFetchStartAdmission admits without waiting, so the test exercises the
// instrumentation bracket rather than the governor itself.
type immediateFetchStartAdmission struct{}

func (immediateFetchStartAdmission) Wait(context.Context) error { return nil }

func rejectingEmitter(rule string) ingest.BatchEmitter {
	return emitFunc(func(
		context.Context,
		yagocrawlcontract.DocumentIngest,
		[]yagomodel.RWIPosting,
		yagomodel.URIMetadataRow,
		ingest.Envelope,
	) error {
		return &ingest.RejectedError{Rule: rule}
	})
}

// A page the node refused to store is fetched, not indexed. Counting it as
// indexed made the crawl monitor's indexed column, and every total derived from
// it, report documents the index does not hold.
func TestPipelineDoesNotIndexAPageTheNodeRejected(t *testing.T) {
	frontier := newRecordingFrontier()
	observer := &countingPipelineObserver{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		rejectingEmitter("too-few-words"),
		pipeline.WithObserver(observer),
	)

	done := runOneJob(t, p, frontier)

	if done.outcome.Indexed != 0 {
		t.Fatalf("indexed = %d, want the refused page uncounted", done.outcome.Indexed)
	}
	if done.outcome.Fetched == 0 {
		t.Fatalf("fetched = %d, want the page still counted as fetched", done.outcome.Fetched)
	}
	if done.outcome.Failed != 0 {
		t.Fatalf("failed = %d, want a refusal not treated as a fetch failure", done.outcome.Failed)
	}
	if !strings.Contains(done.reason, "too-few-words") {
		t.Fatalf("outcome reason = %q, want the gate rule named", done.reason)
	}
	if observer.ingestPublished != 0 {
		t.Fatalf(
			"ingest published = %d, want none for a refused document",
			observer.ingestPublished,
		)
	}
}

// The rule is optional: a node that refuses without naming one still yields a
// usable outcome reason.
func TestPipelineNamesAnUnattributedNodeRejection(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		rejectingEmitter(""),
	)

	done := runOneJob(t, p, frontier)

	if done.outcome.Indexed != 0 || done.reason == "" {
		t.Fatalf("outcome = %#v reason = %q", done.outcome, done.reason)
	}
}

// The fetch-admission wait is instrumented so a rate-limited crawl is
// distinguishable from a busy one; without a governor wired it still brackets
// cleanly and reports zero waiting jobs afterwards.
func TestPipelineRecordsFetchAdmissionWait(t *testing.T) {
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
		pipeline.WithFetchStartAdmission(immediateFetchStartAdmission{}),
	)

	runOneJob(t, p, frontier)

	if observer.admissionWaits != 1 {
		t.Fatalf("admission waits = %d, want 1", observer.admissionWaits)
	}
	if observer.admissionPeak != 1 || observer.admissionActive != 0 {
		t.Fatalf(
			"admission gauge peak = %d active = %d, want a balanced bracket",
			observer.admissionPeak,
			observer.admissionActive,
		)
	}
}
