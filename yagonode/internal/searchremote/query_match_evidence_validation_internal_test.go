package searchremote

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestAppliedEvidenceRequirementsRejectInvalidDefinitions(t *testing.T) {
	tooMany := make([]string, maximumAppliedEvidenceRequirements+1)
	for index := range tooMany {
		tooMany[index] = "term"
	}
	for _, requirements := range [][]string{nil, tooMany, {"term", " "}} {
		if validAppliedEvidenceRequirements(requirements) {
			t.Fatalf("invalid requirements accepted: %#v", requirements)
		}
	}
}

func TestMorphologyEvidenceBindingRejectsInvalidRequirements(t *testing.T) {
	for _, requirements := range [][2]string{{"", "term"}, {"term", " "}} {
		if binding := singleWordMorphologyQueryMatchEvidenceBinding(
			requirements[0],
			requirements[1],
		); binding.valid() {
			t.Fatalf("invalid morphology binding accepted: %#v", binding)
		}
	}
	for _, binding := range []queryMatchEvidenceBinding{
		{wireRequirements: []string{" term"}, rankingRequirements: []string{" term"}},
		{wireRequirements: []string{"term"}, rankingRequirements: []string{" "}},
	} {
		if binding.valid() {
			t.Fatalf("invalid structural binding accepted: %#v", binding)
		}
	}
}

func TestEvidenceRequirementBindingRejectsMissingOrMismatchedHashes(t *testing.T) {
	for _, request := range []yagoproto.SearchRequest{
		{},
		{Query: []yagomodel.Hash{yagomodel.WordHash("term")}},
	} {
		if queryMatchEvidenceRequirementsBound(request, []string{"term", "other"}) {
			t.Fatalf("unbound requirements accepted: %#v", request)
		}
	}
}
