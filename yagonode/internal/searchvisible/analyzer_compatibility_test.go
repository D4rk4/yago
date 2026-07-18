package searchvisible

import (
	"reflect"
	"testing"
)

func TestAnalyzerCompatibilityUsesStoredAnalyzerPolicy(t *testing.T) {
	if !AnalyzerAvailable("en") || AnalyzerAvailable("missing") {
		t.Fatal("analyzer availability policy mismatch")
	}
	if !AnalyzerCompatible("en", "en", Text{Snippet: "alpha beta"}) ||
		AnalyzerCompatible("en", "ru", Text{Snippet: "чрезвычайные полномочия"}) {
		t.Fatal("analyzer compatibility policy mismatch")
	}
}

func TestAnalyzerRequirementOrdinalsUseStoredAnalyzer(t *testing.T) {
	ordinals, available := AnalyzerRequirementOrdinals("en", []string{"alpha", "beta"})
	if !available || !reflect.DeepEqual(ordinals, []int{0, 1}) {
		t.Fatalf("available=%v ordinals=%v", available, ordinals)
	}
}
