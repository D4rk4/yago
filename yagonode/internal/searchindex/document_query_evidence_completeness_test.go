package searchindex

import (
	"reflect"
	"testing"
)

func TestStoredEvidenceRequirementOrdinalsFollowNamedAnalyzer(t *testing.T) {
	ordinals, available := StoredEvidenceRequirementOrdinals(
		"en",
		[]string{"alpha", "beta"},
	)
	if !available || !reflect.DeepEqual(ordinals, []int{0, 1}) {
		t.Fatalf("available=%v ordinals=%v", available, ordinals)
	}
	if _, available := StoredEvidenceRequirementOrdinals("missing", []string{"alpha"}); available {
		t.Fatal("missing analyzer was available")
	}
	if _, available := StoredEvidenceRequirementOrdinals("en", nil); available {
		t.Fatal("empty requirements were available")
	}
	if _, available := StoredEvidenceRequirementOrdinals("en", []string{"the"}); available {
		t.Fatal("fallback analyzer changed the negotiated analyzer identity")
	}
}
