package searchremote

import (
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
