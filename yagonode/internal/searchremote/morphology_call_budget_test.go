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
		peerJobsWithinCallBudget(jobs, newRemoteQueryBudget())
	}
}
