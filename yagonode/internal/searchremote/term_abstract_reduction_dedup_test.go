package searchremote

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestTermAbstractReductionCountsDuplicateResourceOnce(t *testing.T) {
	term := hashFor("duplicate-abstract-term")
	resource := hashFor("duplicate-abstract-resource")
	reduction := termAbstractReduction{
		outcomes:    make([]peerAbstractOutcome, 1),
		entryLimits: []int{2},
		abstracts:   map[yagomodel.Hash]map[yagomodel.Hash]struct{}{},
	}
	reduction.accept(peerSearchCompletion{result: peerSearchResult{
		term: term,
		response: yagoproto.SearchResponse{IndexAbstract: map[yagomodel.Hash]string{
			term: yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{resource, resource}),
		}},
	}})
	if reduction.retainedEntries != 1 || len(reduction.abstracts[term]) != 1 {
		t.Fatalf("duplicate abstract reduction = %#v", reduction)
	}
}

func TestTermAbstractReductionReportsTransportAndAbstractFailures(t *testing.T) {
	term := hashFor("failed-abstract-term")
	peer := searchSeed(t, "failed-abstract-peer")
	reduction := termAbstractReduction{
		outcomes: []peerAbstractOutcome{
			{term: term, peer: peer, responseErr: errors.New("transport")},
			{term: term, peer: peer, abstractErr: errors.New("abstract"), responded: true},
		},
		catalog: termAbstractCatalog{},
	}
	_, failures := reduction.finish(
		[]termPeerTargets{{term: term, peers: []yagomodel.Seed{peer}}},
		nil,
	)
	if len(failures) != 2 {
		t.Fatalf("failures = %#v", failures)
	}
}
