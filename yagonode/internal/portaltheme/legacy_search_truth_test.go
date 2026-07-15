package portaltheme

import (
	"strings"
	"testing"
)

func TestLegacyDefaultResultsGainIncompleteAndFacetScopeState(t *testing.T) {
	body := legacyIncompleteSuggestionFragment + "\n" + legacySearchWindowFragment +
		"\n<fieldset><legend>{{title}}</legend>"
	repaired := repairLegacyPortalDocument(PageResults, body)
	for _, want := range []string{
		"results.incomplete",
		"results.federationUnavailable",
		"identified peer response(s) failed",
		"No results are currently available",
		"counts from {{scope}}",
	} {
		if !strings.Contains(repaired, want) {
			t.Fatalf("repaired result theme missing %q: %s", want, repaired)
		}
	}
	if strings.Contains(repaired, "peer(s) unreachable or timed out") {
		t.Fatalf("repaired result theme kept aggregate-as-peer wording: %s", repaired)
	}
}

func TestSearchTruthRepairLeavesCustomThemeUntouched(t *testing.T) {
	body := `<p>{{results.totalResults}}</p>`
	if repaired := repairLegacySearchTruth(body); repaired != body {
		t.Fatalf("custom theme changed from %q to %q", body, repaired)
	}
}
