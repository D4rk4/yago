package searchremote

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestPrimaryIndexAbstractCatalogUsesSuccessfulPrimaryResponses(t *testing.T) {
	alpha := yagomodel.WordHash("alpha")
	beta := yagomodel.WordHash("beta")
	missing := yagomodel.WordHash("missing")
	shared := hashFor("shared")
	peer := searchSeed(t, "primary-peer")
	budget := newRemoteQueryBudget()
	budget.abstractEntriesRemaining = 2
	catalog, failures := primaryIndexAbstractCatalogWithinBudget([]peerSearchResult{
		{
			peer: peer,
			response: yagoproto.SearchResponse{IndexAbstract: map[yagomodel.Hash]string{
				alpha: yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{shared}),
				beta:  yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{shared}),
			}},
		},
		{peer: searchSeed(t, "failed-peer"), err: errors.New("failed")},
	}, []yagomodel.Hash{alpha, beta, missing}, budget)
	if len(failures) != 0 || budget.abstractEntriesRemaining != 0 {
		t.Fatalf("catalog failures = %#v, budget = %#v", failures, budget)
	}
	if !catalog.peerAdmitted(peer, alpha, shared) ||
		!catalog.peerAdmitted(peer, beta, shared) ||
		catalog.peerReported(peer, missing) {
		t.Fatalf("catalog = %#v", catalog)
	}
	targets := primaryIndexAbstractTargets(
		[]peerSearchResult{{peer: peer}},
		[]yagomodel.Hash{alpha, missing},
		catalog,
	)
	if len(targets) != 2 || len(targets[0].peers) != 1 || len(targets[1].peers) != 0 {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestPrimaryIndexAbstractCatalogRejectsMalformedAndHonorsZeroBudget(t *testing.T) {
	term := yagomodel.WordHash("term")
	peer := searchSeed(t, "primary-peer")
	result := peerSearchResult{
		peer: peer,
		response: yagoproto.SearchResponse{IndexAbstract: map[yagomodel.Hash]string{
			term: "{bad",
		}},
	}
	budget := newRemoteQueryBudget()
	_, failures := primaryIndexAbstractCatalogWithinBudget(
		[]peerSearchResult{result},
		[]yagomodel.Hash{term},
		budget,
	)
	if len(failures) != 1 {
		t.Fatalf("malformed failures = %#v", failures)
	}
	budget.abstractEntriesRemaining = 0
	catalog, failures := primaryIndexAbstractCatalogWithinBudget(
		[]peerSearchResult{result},
		[]yagomodel.Hash{term},
		budget,
	)
	if len(failures) != 0 || catalog.peerReported(peer, term) {
		t.Fatalf("zero-budget catalog = %#v, failures = %#v", catalog, failures)
	}
}
