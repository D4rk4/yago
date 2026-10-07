package searchremote

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
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

func TestTermAbstractReductionWithTargetsButNoAnswersIsUnavailable(t *testing.T) {
	term := hashFor("empty-abstract-term")
	peer := searchSeed(t, "empty-abstract-peer")
	reduction := termAbstractReduction{
		outcomes:  nil,
		abstracts: map[yagomodel.Hash]map[yagomodel.Hash]struct{}{},
	}
	_, failures := reduction.finish(
		[]termPeerTargets{{term: term, peers: []yagomodel.Seed{peer}}},
		nil,
	)
	if len(failures) != 1 ||
		failures[0].Source != searchcore.PartialFailureSourceRemoteYaCy ||
		failures[0].Diagnostic != (searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemoteSearch,
			Cause: searchcore.FailureCauseUnavailable,
		}) {
		t.Fatalf("empty abstract response failures = %#v", failures)
	}
}

func TestTermAbstractReductionKeepsRespondedEmptyAbstractAsComplete(t *testing.T) {
	term := hashFor("complete-empty-abstract-term")
	peer := searchSeed(t, "complete-empty-abstract-peer")
	reduction := termAbstractReduction{
		outcomes: []peerAbstractOutcome{{
			term:      term,
			peer:      peer,
			responded: true,
		}},
		abstracts: map[yagomodel.Hash]map[yagomodel.Hash]struct{}{},
	}
	_, failures := reduction.finish(
		[]termPeerTargets{{term: term, peers: []yagomodel.Seed{peer}}},
		nil,
	)
	if len(failures) != 0 {
		t.Fatalf("complete empty abstract response failures = %#v", failures)
	}
}

func TestTermAbstractReductionKeepsNoPeersAsNoTarget(t *testing.T) {
	term := hashFor("no-peer-abstract-term")
	reduction := termAbstractReduction{
		outcomes:  nil,
		abstracts: map[yagomodel.Hash]map[yagomodel.Hash]struct{}{},
	}
	_, failures := reduction.finish(
		[]termPeerTargets{{term: term}},
		nil,
	)
	if len(failures) != 1 ||
		failures[0].Diagnostic != (searchcore.PartialFailureDiagnostic{
			Stage: searchcore.FailureStageRemoteSearch,
			Cause: searchcore.FailureCauseNoTarget,
		}) {
		t.Fatalf("no-peer abstract failures = %#v", failures)
	}
}
