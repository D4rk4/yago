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

// A theme saved before the fix keeps printing "Nothing found." underneath a
// banner that already said no source answered. Repair rewrites the saved body
// so an operator's customised portal stops contradicting itself.
func TestRepairLegacySearchTruthStopsTheContradictoryEmptyState(t *testing.T) {
	repaired := repairLegacySearchTruth(legacyNothingFoundFragment)
	if repaired != currentNothingFoundFragment {
		t.Fatalf("repaired = %q, want %q", repaired, currentNothingFoundFragment)
	}
}

// Repair runs on every load of a saved theme, so it has to be idempotent. A
// fragment that already carries the fix must come back untouched; rewriting it
// a second time would nest the guard and hide the empty state for good.
func TestRepairLegacySearchTruthLeavesAnAlreadyRepairedThemeAlone(t *testing.T) {
	repaired := repairLegacySearchTruth(currentNothingFoundFragment)
	if repaired != currentNothingFoundFragment {
		t.Fatalf("repair changed an already-current theme: %q", repaired)
	}
}

// A theme that never carried the empty-state line at all must not grow one.
func TestRepairLegacySearchTruthAddsNothingToAnUnrelatedTheme(t *testing.T) {
	const unrelated = `<p class="meta">{{query}}</p>`
	if repaired := repairLegacySearchTruth(unrelated); repaired != unrelated {
		t.Fatalf("repair rewrote an unrelated theme: %q", repaired)
	}
}
