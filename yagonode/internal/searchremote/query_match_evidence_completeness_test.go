package searchremote

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteEvidenceAcceptsExplicitOneOfManyCoverage(t *testing.T) {
	hash := hashFor("complete-one-of-many")
	item := oneOfManyRemoteEvidence()
	result := resultWithQueryMatchEvidence(
		[]string{"alpha", "beta", "gamma"},
		searchcore.Result{
			URLHash:  hash.String(),
			URL:      "https://example.test/alpha",
			Title:    "Alpha",
			Language: "en",
		},
		mapEvidence(hash, item),
	)
	if !result.EvidenceReady ||
		!reflect.DeepEqual(result.EvidenceRequirementOrdinals, []int{0, 1, 2}) ||
		!reflect.DeepEqual(result.FieldTermPositions["body"]["alpha"], []int{1}) ||
		len(result.FieldTermPositions["body"]["beta"]) != 0 ||
		len(result.FieldTermPositions["body"]["gamma"]) != 0 {
		t.Fatalf("one-of-many result = %#v", result)
	}
}

func TestRemoteEvidenceRejectsIncompleteOrFalseOrdinalClaims(t *testing.T) {
	hash := hashFor("malicious-completeness")
	cases := []func(*yagoproto.QueryMatchEvidence){
		func(item *yagoproto.QueryMatchEvidence) {
			item.RequirementOrdinals = []int{0}
			item.AbsentOrdinals = []int{}
		},
		func(item *yagoproto.QueryMatchEvidence) { item.AbsentOrdinals = []int{} },
		func(item *yagoproto.QueryMatchEvidence) { item.AbsentOrdinals = []int{1} },
		func(item *yagoproto.QueryMatchEvidence) {
			item.AbsentOrdinals = []int{0, 1, 2}
		},
		func(item *yagoproto.QueryMatchEvidence) {
			item.RequirementOrdinals = []int{0, 2, 1}
		},
		func(item *yagoproto.QueryMatchEvidence) { item.AbsentOrdinals = nil },
		func(item *yagoproto.QueryMatchEvidence) { item.AbsentOrdinals = []int{2, 1} },
	}
	for index, mutate := range cases {
		item := oneOfManyRemoteEvidence()
		mutate(&item)
		result := resultWithQueryMatchEvidence(
			[]string{"alpha", "beta", "gamma"},
			searchcore.Result{
				URLHash:  hash.String(),
				URL:      "https://example.test/alpha",
				Title:    "Alpha",
				Language: "en",
			},
			mapEvidence(hash, item),
		)
		if result.EvidenceReady {
			t.Fatalf("malformed completeness case %d applied: %#v", index, result)
		}
	}
}

func TestRemoteEvidenceRejectsAnalyzerIrrelevantOrdinalClaims(t *testing.T) {
	hash := hashFor("irrelevant-claim")
	base := yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            "en",
		RequirementOrdinals: []int{0, 2},
		AbsentOrdinals:      []int{2},
		FieldPositions: []yagoproto.QueryFieldPositions{{
			Field: "body",
			Requirements: []yagoproto.QueryRequirementPositions{{
				Ordinal: 0, Positions: []int{1},
			}},
		}},
	}
	for index, mutate := range []func(*yagoproto.QueryMatchEvidence){
		func(item *yagoproto.QueryMatchEvidence) { item.AbsentOrdinals = []int{1, 2} },
		func(item *yagoproto.QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Ordinal = 1
			item.AbsentOrdinals = []int{0, 2}
		},
	} {
		item := base
		item.AbsentOrdinals = append([]int{}, base.AbsentOrdinals...)
		item.FieldPositions = cloneQueryFieldPositions(base.FieldPositions)
		mutate(&item)
		result := resultWithQueryMatchEvidence(
			[]string{"alpha", "the", "gamma"},
			searchcore.Result{
				URLHash:  hash.String(),
				URL:      "https://example.test/alpha",
				Title:    "Alpha",
				Language: "en",
			},
			mapEvidence(hash, item),
		)
		if result.EvidenceReady {
			t.Fatalf("irrelevant ordinal case %d applied: %#v", index, result)
		}
	}
}

func TestRemoteEvidenceMergesOneRequirementAcrossFields(t *testing.T) {
	hash := hashFor("merge-fields")
	item := oneOfManyRemoteEvidence()
	item.FieldPositions = append(item.FieldPositions, yagoproto.QueryFieldPositions{
		Field: "title",
		Requirements: []yagoproto.QueryRequirementPositions{{
			Ordinal: 0, Positions: []int{1},
		}},
	})
	result := resultWithQueryMatchEvidence(
		[]string{"alpha", "beta", "gamma"},
		searchcore.Result{
			URLHash:  hash.String(),
			URL:      "https://example.test/alpha",
			Title:    "Alpha",
			Language: "en",
		},
		mapEvidence(hash, item),
	)
	if !result.EvidenceReady || len(result.FieldTermPositions) != 2 {
		t.Fatalf("merged result = %#v", result)
	}
}

func oneOfManyRemoteEvidence() yagoproto.QueryMatchEvidence {
	return yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            "en",
		RequirementOrdinals: []int{0, 1, 2},
		AbsentOrdinals:      []int{1, 2},
		FieldPositions: []yagoproto.QueryFieldPositions{{
			Field: "body",
			Requirements: []yagoproto.QueryRequirementPositions{{
				Ordinal: 0, Positions: []int{1},
			}},
		}},
	}
}

func mapEvidence(
	hash yagomodel.Hash,
	item yagoproto.QueryMatchEvidence,
) map[yagomodel.Hash]yagoproto.QueryMatchEvidence {
	return map[yagomodel.Hash]yagoproto.QueryMatchEvidence{hash: item}
}
