package yagonode

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type extractionRecrawlBatchReader struct {
	batch        documentstore.StoredDocumentBatch
	err          error
	continuation string
	limit        int
}

func (reader *extractionRecrawlBatchReader) ReadStoredDocumentBatch(
	_ context.Context,
	continuation string,
	limit int,
) (documentstore.StoredDocumentBatch, error) {
	reader.continuation = continuation
	reader.limit = limit

	return reader.batch, reader.err
}

type extractionRecrawlDispatch struct {
	request  crawldispatch.OperatorRequest
	key      string
	accepted crawldispatch.Accepted
	err      error
	calls    int
}

const testExtractionRecrawlActionID = "AAAAAAAAAAAAAAAAAAAAAAAAAA"

func (dispatch *extractionRecrawlDispatch) Dispatch(
	_ context.Context,
	request crawldispatch.OperatorRequest,
	key string,
) (crawldispatch.Accepted, error) {
	dispatch.calls++
	dispatch.request = request
	dispatch.key = key

	return dispatch.accepted, dispatch.err
}

func TestExtractionRecrawlQueuesOnlyMissingOrOlderDocuments(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{batch: documentstore.StoredDocumentBatch{
		Examined: 5,
		Documents: []documentstore.Document{
			{NormalizedURL: "https://example.test/legacy-a"},
			{
				NormalizedURL:        "https://example.test/current",
				ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
			},
			{
				NormalizedURL:        "https://example.test/future",
				ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration + 1,
			},
			{NormalizedURL: "https://example.test/legacy-b"},
		},
		Continuation: "next-position",
	}}
	dispatch := &extractionRecrawlDispatch{accepted: crawldispatch.Accepted{Seeds: 2}}
	result, err := (extractionRecrawlSource{
		documents: reader, dispatcher: dispatch,
	}).QueueOutdatedExtractions(
		t.Context(),
		testExtractionRecrawlActionID,
		"start-position",
		20,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantSeeds := []string{
		"https://example.test/legacy-a",
		"https://example.test/legacy-b",
	}
	if !slices.Equal(dispatch.request.Seeds, wantSeeds) ||
		dispatch.key == "" || dispatch.request.MaxDepth != 0 ||
		!dispatch.request.AllowQueryURLs || dispatch.request.MaxPagesPerHost != 2 ||
		dispatch.request.MaxPagesPerRun == nil || *dispatch.request.MaxPagesPerRun != 2 {
		t.Fatalf("dispatch = %+v key %q", dispatch.request, dispatch.key)
	}
	if result.Examined != 5 || result.Visible != 4 || result.Outdated != 2 ||
		result.CurrentOrNewer != 2 || result.Queued != 2 || !result.Partial ||
		result.Continuation != "next-position" || result.Retry {
		t.Fatalf("result = %+v", result)
	}
}

func TestExtractionRecrawlSkipsDispatchWhenBatchIsCurrent(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{batch: documentstore.StoredDocumentBatch{
		Examined: 1,
		Documents: []documentstore.Document{{
			NormalizedURL:        "https://example.test/current",
			ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		}},
		Complete: true,
	}}
	dispatch := &extractionRecrawlDispatch{}
	result, err := (extractionRecrawlSource{
		documents: reader, dispatcher: dispatch,
	}).QueueOutdatedExtractions(t.Context(), testExtractionRecrawlActionID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.calls != 0 || result.Queued != 0 || result.Outdated != 0 ||
		result.CurrentOrNewer != 1 || result.Partial {
		t.Fatalf("dispatch calls = %d, result = %+v", dispatch.calls, result)
	}
}

func TestExtractionRecrawlRetainsBatchPositionWhenQueueingFails(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{batch: documentstore.StoredDocumentBatch{
		Examined:     1,
		Documents:    []documentstore.Document{{NormalizedURL: "https://example.test/legacy"}},
		Continuation: "next-position",
	}}
	dispatch := &extractionRecrawlDispatch{err: errors.New("queue unavailable")}
	result, err := (extractionRecrawlSource{
		documents: reader, dispatcher: dispatch,
	}).QueueOutdatedExtractions(
		t.Context(),
		testExtractionRecrawlActionID,
		"same-position",
		10,
	)
	if err == nil || !result.Retry || !result.Partial ||
		result.Continuation != "same-position" || result.Queued != 0 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestExtractionRecrawlReportsBoundedReadFailure(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{err: errors.New("read unavailable")}
	result, err := (extractionRecrawlSource{
		documents: reader, dispatcher: &extractionRecrawlDispatch{},
	}).QueueOutdatedExtractions(
		t.Context(),
		testExtractionRecrawlActionID,
		"position",
		7,
	)
	if err == nil || result.Limit != 7 ||
		result.CurrentGeneration != yagocrawlcontract.CurrentExtractionGeneration ||
		!result.Retry || result.Continuation != "position" {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestExtractionRecrawlDispatchKeyIsStableWithinAnAction(t *testing.T) {
	t.Parallel()

	identity := extractionRecrawlBatchIdentity{
		ActionID: testExtractionRecrawlActionID, Continuation: "position",
		NextContinuation: "next-position", Limit: 20, Examined: 20,
		Seeds: []string{"https://example.test/legacy"},
	}
	first := extractionRecrawlDispatchKey(identity)
	if first == "" ||
		first != extractionRecrawlDispatchKey(identity) {
		t.Fatalf("unstable dispatch key = %q", first)
	}
	changedAction := identity
	changedAction.ActionID = "BBBBBBBBBBBBBBBBBBBBBBBBBB"
	if first == extractionRecrawlDispatchKey(changedAction) {
		t.Fatal("new action reused the prior dispatch key")
	}
	changedPosition := identity
	changedPosition.Continuation = "next-position"
	if first == extractionRecrawlDispatchKey(changedPosition) {
		t.Fatal("next batch reused the prior dispatch key")
	}
	changedLimit := identity
	changedLimit.Limit = 100
	if first == extractionRecrawlDispatchKey(changedLimit) {
		t.Fatal("changed bound reused the prior dispatch key")
	}
	changedSeeds := identity
	changedSeeds.Seeds = []string{"https://example.test/replacement"}
	if first == extractionRecrawlDispatchKey(changedSeeds) {
		t.Fatal("changed seed batch reused the prior dispatch key")
	}
}

type ambiguousExtractionRecrawlDispatch struct {
	accepted map[string]crawldispatch.Accepted
	calls    int
}

func (dispatch *ambiguousExtractionRecrawlDispatch) Dispatch(
	_ context.Context,
	request crawldispatch.OperatorRequest,
	key string,
) (crawldispatch.Accepted, error) {
	dispatch.calls++
	if accepted, found := dispatch.accepted[key]; found {
		accepted.Duplicate = true

		return accepted, nil
	}
	accepted := crawldispatch.Accepted{Seeds: len(request.Seeds)}
	dispatch.accepted[key] = accepted
	if dispatch.calls == 1 {
		return accepted, errors.New("queue outcome unavailable")
	}

	return accepted, nil
}

func TestExtractionRecrawlChangedRetryBoundCannotSkipAmbiguousCommit(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{}
	dispatch := &ambiguousExtractionRecrawlDispatch{
		accepted: make(map[string]crawldispatch.Accepted),
	}
	source := extractionRecrawlSource{documents: reader, dispatcher: dispatch}
	reader.batch = extractionRecrawlTestBatch(20)
	first, err := source.QueueOutdatedExtractions(
		t.Context(),
		testExtractionRecrawlActionID,
		"same-position",
		20,
	)
	if err == nil || !first.Retry || first.Continuation != "same-position" {
		t.Fatalf("ambiguous first result = %+v, error=%v", first, err)
	}
	reader.batch = extractionRecrawlTestBatch(100)
	second, err := source.QueueOutdatedExtractions(
		t.Context(),
		testExtractionRecrawlActionID,
		"same-position",
		100,
	)
	if err != nil || second.Queued != 100 || second.AlreadyQueued != 0 ||
		second.Continuation != "position-100" || dispatch.calls != 2 ||
		len(dispatch.accepted) != 2 {
		t.Fatalf("changed-bound retry = %+v, calls=%d keys=%d error=%v",
			second, dispatch.calls, len(dispatch.accepted), err)
	}
}

func extractionRecrawlTestBatch(size int) documentstore.StoredDocumentBatch {
	documents := make([]documentstore.Document, size)
	for index := range size {
		documents[index].NormalizedURL = "https://example.test/legacy-" + strconv.Itoa(index)
	}

	return documentstore.StoredDocumentBatch{
		Examined: size, Documents: documents, Continuation: "position-" + strconv.Itoa(size),
	}
}

func TestExtractionRecrawlReportsIdempotentQueueRetry(t *testing.T) {
	t.Parallel()

	reader := &extractionRecrawlBatchReader{batch: documentstore.StoredDocumentBatch{
		Examined:  1,
		Documents: []documentstore.Document{{NormalizedURL: "https://example.test/legacy"}},
		Complete:  true,
	}}
	dispatch := &extractionRecrawlDispatch{accepted: crawldispatch.Accepted{
		Seeds: 1, Duplicate: true,
	}}
	result, err := (extractionRecrawlSource{
		documents: reader, dispatcher: dispatch,
	}).QueueOutdatedExtractions(t.Context(), testExtractionRecrawlActionID, "", 10)
	if err != nil || result.Queued != 0 || result.AlreadyQueued != 1 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestExtractionRecrawlRequiresLiveDispatcher(t *testing.T) {
	t.Parallel()

	if source := newExtractionRecrawlSource(&extractionRecrawlBatchReader{}, nil); source != nil {
		t.Fatalf("source without dispatcher = %T", source)
	}
}
