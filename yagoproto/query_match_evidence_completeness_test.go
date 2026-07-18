package yagoproto

import "testing"

func TestQueryMatchEvidenceRequiresCompleteOrdinalPartition(t *testing.T) {
	validAbsent := validQueryMatchEvidenceFixture()
	validAbsent.RequirementOrdinals = []int{0, 1}
	validAbsent.AbsentOrdinals = []int{1}
	if !validQueryMatchEvidence(validAbsent) {
		t.Fatalf("complete absent evidence rejected: %#v", validAbsent)
	}
	cases := []func(*QueryMatchEvidence){
		func(item *QueryMatchEvidence) { item.RequirementOrdinals = nil },
		func(item *QueryMatchEvidence) { item.AbsentOrdinals = nil },
		func(item *QueryMatchEvidence) { item.RequirementOrdinals = []int{1, 0} },
		func(item *QueryMatchEvidence) { item.RequirementOrdinals = []int{0, 0} },
		func(item *QueryMatchEvidence) { item.RequirementOrdinals = []int{0, 32} },
		func(item *QueryMatchEvidence) { item.AbsentOrdinals = []int{1} },
		func(item *QueryMatchEvidence) { item.AbsentOrdinals = []int{0} },
		func(item *QueryMatchEvidence) { item.RequirementOrdinals = []int{0, 1} },
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Ordinal = 1
		},
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Positions = nil
		},
		func(item *QueryMatchEvidence) {
			item.RequirementOrdinals = make([]int, maximumResourceRequirements+1)
			for index := range item.RequirementOrdinals {
				item.RequirementOrdinals[index] = index
			}
		},
	}
	for index, mutate := range cases {
		item := validQueryMatchEvidenceFixture()
		mutate(&item)
		if validQueryMatchEvidence(item) {
			t.Fatalf("partial ordinal case %d accepted: %#v", index, item)
		}
	}
}
