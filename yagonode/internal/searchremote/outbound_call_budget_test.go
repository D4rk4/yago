package searchremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestOutboundCallBudgetAcquisitionAndRestoration(t *testing.T) {
	var absent *outboundCallBudget
	if absent.available() != 0 || !absent.acquire() {
		t.Fatal("nil call budget is not unbounded")
	}
	absent.restore()

	budget := newOutboundCallBudget(2)
	if budget.available() != 2 || !budget.acquire() || budget.available() != 1 ||
		!budget.acquire() || budget.acquire() {
		t.Fatalf("call budget remaining = %d", budget.available())
	}
	budget.restore()
	if budget.available() != 1 {
		t.Fatalf("restored call budget = %d", budget.available())
	}

	first := newOutboundCallBudget(1)
	second := newOutboundCallBudget(0)
	if acquireOutboundCall(first, second) || first.available() != 1 {
		t.Fatalf("composite acquisition retained a partial reservation: %d", first.available())
	}
	if !acquireOutboundCall(nil, first) || first.available() != 0 {
		t.Fatalf("composite acquisition remaining = %d", first.available())
	}
}

func TestPeerJobsWithinCallBudgetCapsMorphologyAndAttachesAttemptBudgets(t *testing.T) {
	if got := peerJobsWithinCallBudget(nil, nil); got != nil {
		t.Fatalf("nil query budget jobs = %#v", got)
	}
	if got := peerJobsWithinCallBudget(
		[]peerSearchJob{{}},
		&remoteQueryBudget{},
	); got != nil {
		t.Fatalf("missing call budget jobs = %#v", got)
	}

	budget := &remoteQueryBudget{
		peerCalls:       newOutboundCallBudget(3),
		morphologyCalls: newOutboundCallBudget(1),
	}
	got := peerJobsWithinCallBudget([]peerSearchJob{
		{morphology: true},
		{morphology: true},
		{},
		{},
		{},
	}, budget)
	if len(got) != 3 || !got[0].morphology || got[1].morphology || got[2].morphology {
		t.Fatalf("planned jobs = %#v", got)
	}
	for position, job := range got {
		if job.peerCalls != budget.peerCalls {
			t.Fatalf("job %d query call budget was not attached", position)
		}
		if job.transportAttempts != budget.transportAttempts {
			t.Fatalf("job %d transport attempt budget was not attached", position)
		}
	}
	if got[0].morphologyCalls != budget.morphologyCalls ||
		got[1].morphologyCalls != nil || got[2].morphologyCalls != nil {
		t.Fatalf("morphology budgets = %#v", got)
	}

	budget.peerCalls = newOutboundCallBudget(0)
	if got := peerJobsWithinCallBudget([]peerSearchJob{{}}, budget); len(got) != 0 {
		t.Fatalf("exhausted query budget jobs = %#v", got)
	}
	budget.peerCalls = newOutboundCallBudget(1)
	budget.morphologyCalls = nil
	if got := peerJobsWithinCallBudget(
		[]peerSearchJob{{morphology: true}, {}},
		budget,
	); len(got) != 1 || got[0].morphology {
		t.Fatalf("missing morphology budget jobs = %#v", got)
	}
}

func TestOutboundAttemptBudgetPreventsHTTPCallAndMorphologyAdmissionIsBounded(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()
	target, err := url.Parse(server.URL + yagoproto.PathSearch)
	if err != nil {
		t.Fatal(err)
	}
	remote := searcher{client: server.Client()}
	budget := newOutboundCallBudget(1)
	if err := sendEmptyRemoteSearchWithinLimit(
		remote,
		t.Context(),
		target,
		budget,
	); err != nil {
		t.Fatal(err)
	}
	if err := sendEmptyRemoteSearchWithinLimit(
		remote,
		t.Context(),
		target,
		budget,
	); !errors.Is(err, errRemoteSearchBudgetExhausted) || requests != 1 {
		t.Fatalf("second attempt err=%v requests=%d", err, requests)
	}

	custom := make(chan struct{}, 1)
	if remote.morphologySearchAdmission() != remoteMorphologySearchAdmission {
		t.Fatal("default morphology admission mismatch")
	}
	remote.morphologyAdmission = custom
	if remote.morphologySearchAdmission() != custom {
		t.Fatal("custom morphology admission mismatch")
	}
	custom <- struct{}{}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	result := remote.queryPeerJob(canceled, peerSearchJob{morphology: true})
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled morphology admission = %v", result.err)
	}
}

func TestRemoteSearchRequestLimitRejectsAnExhaustedCallBudget(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)

		return nil, errors.New("unexpected transport call")
	})}
	_, _, err := NewSearcher(Config{Client: client}).(searcher).sendRemoteSearchWithinLimit(
		t.Context(),
		searchSeed(t, "peer"),
		yagoproto.SearchRequest{},
		remoteSearchRequestLimits{
			responseBodyLimit: remoteSearchBodyCap,
			callBudgets:       []*outboundCallBudget{newOutboundCallBudget(0)},
		},
	)
	if !errors.Is(err, errRemoteSearchBudgetExhausted) || requests.Load() != 0 {
		t.Fatalf("exhausted request err/calls = %v/%d", err, requests.Load())
	}
}

func TestMultiAddressFallbackConsumesOneLogicalPeerCallPerPartition(t *testing.T) {
	servers := make([]*httptest.Server, 2)
	peers := make([]yagomodel.Seed, 2)
	for position := range servers {
		servers[position] = httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
			},
		))
		defer servers[position].Close()
		peer := serverSeedWithHash(
			t,
			servers[position].URL,
			hashFor(fmt.Sprintf("partition-%d", position)),
		)
		dead, err := yagomodel.ParseHost("127.0.0.2")
		if err != nil {
			t.Fatal(err)
		}
		fallback, err := yagomodel.ParseIP6("127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		peer.IP = yagomodel.Some(dead)
		peer.IP6 = yagomodel.Some(fallback)
		peers[position] = peer
	}
	remote := NewSearcher(Config{Client: http.DefaultClient}).(searcher)
	budget := newRemoteQueryBudget()
	budget.peerCalls = newOutboundCallBudget(2)
	results := remote.queryPeerJobsWithinBudget(t.Context(), []peerSearchJob{
		{peer: peers[0], request: yagoproto.SearchRequest{}},
		{peer: peers[1], request: yagoproto.SearchRequest{}},
	}, budget)
	if len(results) != 2 || results[0].err != nil || results[1].err != nil {
		t.Fatalf("partition fallback results = %#v", results)
	}
	if budget.peerCalls.available() != 0 {
		t.Fatalf("remaining logical peer calls = %d", budget.peerCalls.available())
	}
}

func TestMultiAddressBodyReadFailureFallsBackAndRecordsReachability(t *testing.T) {
	body := yagoproto.SearchResponse{}.Encode().Encode()
	var attempts atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			if req.URL.Hostname() == "127.0.0.2" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       failingBody{},
					Header:     make(http.Header),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	peer := serverSeedWithHash(t, "http://127.0.0.2:8090", hashFor("fallback"))
	fallback, err := yagomodel.ParseIP6("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some(fallback)
	sink := &recordingPeerReachability{}
	remote := NewSearcher(Config{Client: client, Reachability: sink}).(searcher)
	remote.lifecycle = newPeerLifecycleSession(sink)
	results := remote.queryPeerJobsWithinBudget(
		t.Context(),
		[]peerSearchJob{{peer: peer}},
		newRemoteQueryBudget(),
	)
	remote.lifecycle.flush(t.Context())
	if len(results) != 1 || results[0].err != nil || attempts.Load() != 2 {
		t.Fatalf("fallback results/attempts = %#v/%d", results, attempts.Load())
	}
	if len(sink.reachable) != 1 || sink.reachable[0] != peer.Hash ||
		len(sink.unreachable) != 0 {
		t.Fatalf("fallback lifecycle = reachable %v unreachable %v",
			sink.reachable, sink.unreachable)
	}
}

func TestRemoteQueryTransportAttemptsRemainWithinPhysicalBudget(t *testing.T) {
	var attempts atomic.Int32
	remote := NewSearcher(Config{Client: &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			attempts.Add(1)

			return nil, errors.New("transport unavailable")
		},
	)}}).(searcher)
	peer := serverSeedWithHash(t, "http://127.0.0.1:8090", hashFor("peer"))
	alternatives, err := yagomodel.ParseIP6(
		"127.0.0.2|127.0.0.3|127.0.0.4|127.0.0.5",
	)
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some(alternatives)
	jobs := make([]peerSearchJob, remoteQueryPeerCallBudget)
	for position := range jobs {
		jobs[position] = peerSearchJob{peer: peer}
	}
	budget := newRemoteQueryBudget()
	remote.queryPeerJobsWithinBudget(t.Context(), jobs, budget)
	if attempts.Load() != remoteQueryPeerCallBudget ||
		budget.transportAttempts.available() != 0 {
		t.Fatalf("physical attempts/remaining = %d/%d",
			attempts.Load(), budget.transportAttempts.available())
	}
}

func TestRemoteSearchSplitsPeerDeadlineAcrossRemainingEndpoints(t *testing.T) {
	var primaryCanceled atomic.Bool
	body := yagoproto.SearchResponse{}.Encode().Encode()
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Hostname() == "127.0.0.2" {
				<-req.Context().Done()
				primaryCanceled.Store(true)

				return nil, req.Context().Err()
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	peer := serverSeedWithHash(t, "http://127.0.0.2:8090", hashFor("fallback"))
	fallback, err := yagomodel.ParseIP6("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some(fallback)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err = NewSearcher(Config{Client: client}).(searcher).sendRemoteSearchWithinLimit(
		ctx,
		peer,
		yagoproto.SearchRequest{},
		remoteSearchRequestLimits{responseBodyLimit: remoteSearchBodyCap},
	)
	if err != nil || !primaryCanceled.Load() {
		t.Fatalf("multi-address fallback error/canceled = %v/%t", err, primaryCanceled.Load())
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("multi-address fallback exhausted peer deadline: %s", elapsed)
	}
}
