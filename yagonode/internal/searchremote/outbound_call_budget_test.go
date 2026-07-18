package searchremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
	if _, _, err := remote.sendRemoteSearchToWithinLimit(
		t.Context(),
		target,
		yagoproto.SearchRequest{},
		remoteSearchBodyCap,
		budget,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := remote.sendRemoteSearchToWithinLimit(
		t.Context(),
		target,
		yagoproto.SearchRequest{},
		remoteSearchBodyCap,
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
