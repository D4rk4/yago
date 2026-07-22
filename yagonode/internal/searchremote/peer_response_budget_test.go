package searchremote

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestDistributedLimitsBoundTotalAndPreserveRequestOrder(t *testing.T) {
	if got := distributedLimits(10, 3, 10); !reflect.DeepEqual(got, []int{4, 3, 3}) {
		t.Fatalf("remainder distribution = %v", got)
	}
	if got := distributedLimits(100, 2, 7); !reflect.DeepEqual(got, []int{7, 7}) {
		t.Fatalf("per-response cap = %v", got)
	}
	if got := distributedLimits(0, 0, 1); got != nil {
		t.Fatalf("empty distribution = %v", got)
	}
	responseLimits := distributedLimits(
		remoteQueryResponseByteBudget,
		256,
		remoteSearchBodyCap,
	)
	resultLimits := distributedLimits(remoteQueryResultEntryBudget, 256, 1024)
	abstractLimits := distributedLimits(remoteQueryAbstractEntryBudget, 256, 8192)
	var responseTotal, resultTotal, abstractTotal int
	for position := range responseLimits {
		responseTotal += responseLimits[position]
		resultTotal += resultLimits[position]
		abstractTotal += abstractLimits[position]
	}
	if responseTotal != remoteQueryResponseByteBudget ||
		resultTotal != remoteQueryResultEntryBudget ||
		abstractTotal != remoteQueryAbstractEntryBudget {
		t.Fatalf(
			"256-job totals = response:%d result:%d abstract:%d",
			responseTotal,
			resultTotal,
			abstractTotal,
		)
	}
}

func TestQueryPeerJobsReducesResourcesAsTheyArriveWithinSharedBudget(t *testing.T) {
	secondAnswered := make(chan struct{})
	firstRows := []yagomodel.URIMetadataRow{
		metadataRow(t, hashFor("first-a"), "https://first.example/a", "First A"),
		metadataRow(t, hashFor("first-b"), "https://first.example/b", "First B"),
		metadataRow(t, hashFor("first-c"), "https://first.example/c", "First C"),
	}
	secondRows := []yagomodel.URIMetadataRow{
		metadataRow(t, hashFor("second-a"), "https://second.example/a", "Second A"),
		metadataRow(t, hashFor("second-b"), "https://second.example/b", "Second B"),
	}
	firstBody := yagoproto.SearchResponse{
		References: "discarded",
		Count:      len(firstRows) + 1,
		Resources:  firstRows,
		IndexCount: map[yagomodel.Hash]int{hashFor("term"): 9},
	}.Encode().Encode()
	secondBody := yagoproto.SearchResponse{
		Count:     len(secondRows),
		Resources: secondRows,
	}.Encode().Encode()
	firstResponse := template.Must(template.New("first-response").Parse(firstBody))
	secondResponse := template.Must(template.New("second-response").Parse(secondBody))
	firstServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			<-secondAnswered
			_ = firstResponse.Execute(w, nil)
		}),
	)
	defer firstServer.Close()
	secondServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = secondResponse.Execute(w, nil)
			close(secondAnswered)
		}),
	)
	defer secondServer.Close()

	budget := newRemoteQueryBudget()
	budget.responseBytesRemaining = 1 << 20
	budget.resultEntriesRemaining = 3
	budget.abstractEntriesRemaining = 1
	remote := searcher{
		client:         firstServer.Client(),
		concurrency:    2,
		perPeerTimeout: time.Second,
	}
	results := remote.queryPeerJobsWithinBudget(t.Context(), []peerSearchJob{
		{peer: serverSeed(t, firstServer.URL)},
		{peer: serverSeed(t, secondServer.URL)},
	}, budget)
	if len(results) != 2 || len(results[0].response.Resources) != 2 ||
		len(results[1].response.Resources) != 1 {
		t.Fatalf("retained resources = %#v", results)
	}
	if results[0].response.Resources[0].Properties[yagomodel.URLMetaColDescription] == "" ||
		results[0].response.References != "" || results[0].response.IndexCount != nil {
		t.Fatalf("compacted first response = %#v", results[0].response)
	}
	detached := detachedMetadataRows(firstRows[:1])
	retainedTitle := detached[0].Properties[yagomodel.URLMetaColDescription]
	firstRows[0].Properties[yagomodel.URLMetaColDescription] = "changed"
	if detached[0].Properties[yagomodel.URLMetaColDescription] != retainedTitle {
		t.Fatalf("retained metadata aliases source rows")
	}
	if !results[0].resourcesTruncated || !results[1].resourcesTruncated {
		t.Fatalf("truncation markers = %#v", results)
	}
	if !results[0].responseIncomplete || results[1].responseIncomplete {
		t.Fatalf("incomplete markers = %#v", results)
	}
	if budget.resultEntriesRemaining != 0 ||
		budget.responseBytesRemaining != 1<<20-len(firstBody)-len(secondBody) {
		t.Fatalf("remaining budget = %#v", budget)
	}
}

func TestTermAbstractsReducesCompletionOrderIndependently(t *testing.T) {
	termA := hashFor("term-a")
	termB := hashFor("term-b")
	shared := hashFor("shared-url")
	firstOnly := hashFor("first-only")
	secondOnly := hashFor("second-only")
	thirdOnly := hashFor("third-only")
	firstEncoded := yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{shared, firstOnly})
	secondEncoded := yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{shared, secondOnly})
	thirdEncoded := yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{thirdOnly})
	secondAnswered := make(chan struct{})
	firstServer := abstractResponseServer(t, termA, firstEncoded, func() { <-secondAnswered })
	defer firstServer.Close()
	secondServer := abstractResponseServer(
		t,
		termA,
		secondEncoded,
		func() { close(secondAnswered) },
	)
	defer secondServer.Close()
	thirdServer := abstractResponseServer(t, termB, thirdEncoded, func() {})
	defer thirdServer.Close()

	budget := newRemoteQueryBudget()
	budget.responseBytesRemaining = 1 << 20
	budget.resultEntriesRemaining = 1
	budget.abstractEntriesRemaining = 5
	remote := searcher{
		client:         firstServer.Client(),
		concurrency:    3,
		perPeerTimeout: time.Second,
	}
	abstracts, failures := remote.termAbstractsWithinBudget(
		t.Context(),
		searchcore.Request{},
		[]termPeerTargets{
			{term: termA, peers: []yagomodel.Seed{
				serverSeed(t, firstServer.URL),
				serverSeed(t, secondServer.URL),
			}},
			{term: termB, peers: []yagomodel.Seed{serverSeed(t, thirdServer.URL)}},
		},
		nil,
		budget,
	)
	if len(failures) != 0 || len(abstracts[termA]) != 3 || len(abstracts[termB]) != 1 {
		t.Fatalf("abstract reduction = %#v failures=%#v", abstracts, failures)
	}
	for _, hash := range []yagomodel.Hash{shared, firstOnly, secondOnly} {
		if _, found := abstracts[termA][hash]; !found {
			t.Fatalf("term A missing %s: %#v", hash, abstracts[termA])
		}
	}
	if _, found := abstracts[termB][thirdOnly]; !found {
		t.Fatalf("term B missing %s: %#v", thirdOnly, abstracts[termB])
	}
	if budget.abstractEntriesRemaining != 1 {
		t.Fatalf("abstract entries remaining = %d", budget.abstractEntriesRemaining)
	}
}

func TestTermAbstractReductionSkipsDecodeWithoutEntryBudget(t *testing.T) {
	term := hashFor("zero-budget-term")
	reduction := termAbstractReduction{
		outcomes:    make([]peerAbstractOutcome, 1),
		entryLimits: []int{0},
		abstracts:   map[yagomodel.Hash]map[yagomodel.Hash]struct{}{},
	}
	reduction.accept(peerSearchCompletion{result: peerSearchResult{
		term: term,
		response: yagoproto.SearchResponse{IndexAbstract: map[yagomodel.Hash]string{
			term: "malformed",
		}},
	}})
	if reduction.outcomes[0].abstractErr != nil || reduction.retainedEntries != 0 ||
		len(reduction.abstracts) != 0 {
		t.Fatalf("zero-budget reduction = %#v", reduction)
	}
}

func TestRemoteSearchAdmissionIsSharedAndCancellationAware(t *testing.T) {
	if cap(remoteSearchFetchAdmission) != DefaultConcurrency {
		t.Fatalf("process admission capacity = %d", cap(remoteSearchFetchAdmission))
	}
	admission := make(chan struct{}, 1)
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	validBody := yagoproto.SearchResponse{}.Encode().Encode()
	first := searcher{
		fetchAdmission: admission,
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			close(firstStarted)
			<-firstRelease
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(validBody)),
				Header:     make(http.Header),
			}, nil
		})},
	}
	var secondAttempts atomic.Int32
	second := searcher{
		fetchAdmission: admission,
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondAttempts.Add(1)
			return nil, errors.New("unexpected transport call")
		})},
	}
	target, err := url.Parse("http://127.0.0.1:8090/yacy/search.html")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		firstTarget := *target
		err := sendEmptyRemoteSearchWithinLimit(
			first,
			context.Background(),
			&firstTarget,
		)
		firstDone <- err
	}()
	<-firstStarted
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	secondTarget := *target
	err = sendEmptyRemoteSearchWithinLimit(
		second,
		ctx,
		&secondTarget,
	)
	if !errors.Is(err, errRemoteSearchAdmissionCanceled) ||
		!errors.Is(err, context.DeadlineExceeded) || secondAttempts.Load() != 0 {
		t.Fatalf("saturated admission err=%v attempts=%d", err, secondAttempts.Load())
	}
	close(firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("first request: %v", err)
	}
	if len(admission) != 0 {
		t.Fatalf("admission slots retained = %d", len(admission))
	}
}

func TestRemoteSearchBudgetErrorsDoNotPenalizePeers(t *testing.T) {
	peer := yagomodel.Seed{Hash: hashFor("peer")}
	session := &reputationSession{
		observations: reputationObservationSinkFunc(func(
			context.Context,
			[]peerreputation.Observation,
		) {
		}),
	}
	recordPeerFailure(session, peer, errRemoteSearchBudgetExhausted)
	recordPeerFailure(session, peer, errRemoteSearchAdmissionCanceled)
	if len(session.pending) != 0 {
		t.Fatalf("local-limit observations = %#v", session.pending)
	}
	recordPeerFailure(session, peer, errors.New("transport failed"))
	if len(session.pending) != 1 {
		t.Fatalf("transport observations = %#v", session.pending)
	}
}

func TestRemoteSearchResponseBudgetRejectsOnlyTheBoundedResponse(t *testing.T) {
	body := yagoproto.SearchResponse{References: strings.Repeat("x", 64)}.Encode().Encode()
	_, responseBytes, err := readRemoteSearchResponseWithinLimit(strings.NewReader(body), 8)
	if !errors.Is(err, errRemoteSearchBudgetExhausted) || responseBytes != 8 {
		t.Fatalf("bounded read bytes=%d err=%v", responseBytes, err)
	}
	result := (searcher{}).queryPeerJob(t.Context(), peerSearchJob{
		peer:                yagomodel.Seed{Hash: hashFor("peer")},
		responseBodyLimited: true,
	})
	if !errors.Is(result.err, errRemoteSearchBudgetExhausted) {
		t.Fatalf("zero-budget job error = %v", result.err)
	}
}

func abstractResponseServer(
	t *testing.T,
	term yagomodel.Hash,
	abstract string,
	beforeWrite func(),
) *httptest.Server {
	t.Helper()
	body := yagoproto.SearchResponse{
		IndexAbstract: map[yagomodel.Hash]string{term: abstract},
	}.Encode().Encode()
	response := template.Must(template.New("abstract-response").Parse(body))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		beforeWrite()
		_ = response.Execute(w, nil)
	}))
}

type reputationObservationSinkFunc func(context.Context, []peerreputation.Observation)

func (observe reputationObservationSinkFunc) Observe(
	ctx context.Context,
	observations []peerreputation.Observation,
) {
	observe(ctx, observations)
}
