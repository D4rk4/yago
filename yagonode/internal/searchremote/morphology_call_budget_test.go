package searchremote

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestMaximumMorphologyPlanCapsActualCallsAndProcessConcurrency(t *testing.T) {
	var total atomic.Int32
	var abstractTotal atomic.Int32
	var active atomic.Int32
	var maximumActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)
		abstract := r.URL.Query().Get(yagoproto.FieldAbstracts)
		if abstract == "" || abstract == string(yagoproto.SearchAbstractsAuto) {
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
			return
		}
		abstractTotal.Add(1)
		current := active.Add(1)
		for observed := maximumActive.Load(); current > observed; observed = maximumActive.Load() {
			if maximumActive.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		term := yagomodel.Hash(abstract)
		writeFixtureResponse(t, w, yagoproto.SearchResponse{
			IndexCount:    map[yagomodel.Hash]int{term: 0},
			IndexAbstract: map[yagomodel.Hash]string{term: "{}"},
		}.Encode().Encode())
	}))
	defer server.Close()

	peers := make([]yagomodel.Seed, 8)
	for position := range peers {
		peers[position] = serverSeedWithHash(
			t,
			server.URL,
			hashFor("bounded-peer-"+strconv.Itoa(position)),
		)
	}
	_, err := NewSearcher(Config{
		Client:         server.Client(),
		NetworkName:    "freeworld",
		Peers:          fakePeerSource{peers: peers},
		MaxPeers:       len(peers),
		Redundancy:     len(peers),
		Concurrency:    DefaultConcurrency,
		PerPeerTimeout: time.Second,
		OverallTimeout: 2 * time.Second,
		ExpandWord: func(word string) []string {
			forms := make([]string, 64)
			for position := range forms {
				forms[position] = word + "-" + strconv.Itoa(position)
			}

			return forms
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  "one two three four five six",
		Terms:  []string{"one", "two", "three", "four", "five", "six"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := abstractTotal.Load(); got != remoteMorphologyPeerCallBudget {
		t.Fatalf("abstract attempts = %d, want %d", got, remoteMorphologyPeerCallBudget)
	}
	if got := total.Load(); got > remoteQueryPeerCallBudget {
		t.Fatalf("total attempts = %d, limit %d", got, remoteQueryPeerCallBudget)
	}
	if got := maximumActive.Load(); got < 2 || got > remoteMorphologyConcurrency {
		t.Fatalf("morphology concurrency = %d", got)
	}
}

func TestRemoteSearcherReportsCallBudgetOmittedVariants(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	peers := make([]yagomodel.Seed, 32)
	for partition := range 1 << 4 {
		for candidate := range 2 {
			position := partition*2 + candidate
			peers[position] = serverSeedWithHash(
				t,
				server.URL,
				partitionSearchHash(partition, candidate),
			)
		}
	}
	response, err := NewSearcher(Config{
		Client:             server.Client(),
		NetworkName:        "freeworld",
		Peers:              fakePeerSource{peers: peers},
		MaxPeers:           len(peers),
		Redundancy:         len(peers),
		MinimumPeerAgeDays: -1,
		MinimumPeerRWIs:    -1,
		PartitionExponent:  4,
		ExpandWord: func(word string) []string {
			return []string{word + "s", word + "ing"}
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  "run",
		Terms:  []string{"run"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if attempts.Load() != remoteQueryPeerCallBudget {
		t.Fatalf(
			"peer attempts = %d, want call budget %d",
			attempts.Load(),
			remoteQueryPeerCallBudget,
		)
	}
	if len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, want an empty partial result", response)
	}
	failure := response.PartialFailures[0]
	if failure.Source != searchcore.PartialFailureSourceRemoteStage ||
		failure.Diagnostic != (searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemoteStage,
			Cause: searchcore.FailureCauseBudget,
		}) {
		t.Fatalf("call-budget failure = %+v, want local budget omission", failure)
	}
}

func TestRemoteSearcherReportsBudgetOmittedAbstractJobs(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	peers := make([]yagomodel.Seed, 32)
	for partition := range 1 << 4 {
		for candidate := range 2 {
			position := partition*2 + candidate
			peers[position] = serverSeedWithHash(
				t,
				server.URL,
				partitionSearchHash(partition, candidate),
			)
		}
	}
	response, err := NewSearcher(Config{
		Client:             server.Client(),
		NetworkName:        "freeworld",
		Peers:              fakePeerSource{peers: peers},
		MaxPeers:           len(peers),
		Redundancy:         len(peers),
		MinimumPeerAgeDays: -1,
		MinimumPeerRWIs:    -1,
		PartitionExponent:  4,
		ExpandWord: func(word string) []string {
			forms := make([]string, 11)
			for position := range forms {
				forms[position] = word + "-variant-" + strconv.Itoa(position)
			}

			return forms
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  "alpha beta",
		Terms:  []string{"alpha", "beta"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if attempts.Load() != remoteQueryPeerCallBudget {
		t.Fatalf(
			"peer attempts = %d, want call budget %d",
			attempts.Load(),
			remoteQueryPeerCallBudget,
		)
	}
	if len(response.Results) != 0 {
		t.Fatalf("results = %#v, want none from empty peers", response.Results)
	}
	budgetFailure := false
	for _, failure := range response.PartialFailures {
		if failure.Diagnostic.Stage == searchcore.FailureStageRemoteStage &&
			failure.Diagnostic.Cause == searchcore.FailureCauseBudget {
			budgetFailure = true
		}
		if failure.Source == searchcore.PartialFailureSourceRemoteYaCy &&
			failure.Diagnostic.Cause == searchcore.FailureCauseUnavailable {
			t.Fatalf("local abstract omission blamed a remote peer source: %+v", failure)
		}
	}
	if !budgetFailure {
		t.Fatalf("abstract response = %#v, want a local budget failure", response)
	}
}

func BenchmarkBoundedMaximumMorphologyPlan(b *testing.B) {
	remote := searcher{expandWord: func(word string) []string {
		forms := make([]string, 64)
		for position := range forms {
			forms[position] = word + "-" + strconv.Itoa(position)
		}

		return forms
	}}
	terms := []string{"one", "two", "three", "four", "five", "six"}
	peers := []yagomodel.Seed{{Hash: hashFor("one")}, {Hash: hashFor("two")}}
	for b.Loop() {
		requirements, _ := remote.groupedMorphologyRequirements(terms)
		targets := make([]termPeerTargets, 0, len(requirements))
		for _, term := range distinctRequirementForms(requirements) {
			targets = append(targets, termPeerTargets{term: term, peers: peers})
		}
		jobs := abstractSearchJobs(
			searchcore.Request{},
			boundedMorphologyTargets(targets),
			"freeworld",
			DefaultPerPeerTimeout,
		)
		_, _, _ = peerJobsWithinCallBudget(jobs, newRemoteQueryBudget())
	}
}
