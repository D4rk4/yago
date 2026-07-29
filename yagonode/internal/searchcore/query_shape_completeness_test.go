package searchcore

import "testing"

// TestLostSourceFailuresIgnoresQueryShape pins which failures stand for lost
// coverage. A stage that had nothing to ask because the query carried no words
// lost nothing; counting it as a loss made an empty answer claim the node could
// not vouch for it, and told the caller to retry a query whose outcome is fixed.
func TestLostSourceFailuresIgnoresQueryShape(t *testing.T) {
	t.Parallel()

	shaped := Response{PartialFailures: []PartialFailure{
		{Source: PartialFailureSourceQueryShape, Reason: "no query terms"},
	}}
	if got := shaped.LostSourceFailures(); got != 0 {
		t.Fatalf("lost sources = %d, want a query-shape failure to count for none", got)
	}

	// The over-permitting guard: every other source must still count, or a real
	// loss would be served as a truthful zero.
	for _, source := range []string{
		PartialFailureSourceRemoteYaCy,
		PartialFailureSourceRemoteStage,
		PartialFailureSourcePeerReputation,
		PartialFailureSourceExactStage,
		PartialFailureSourceLocalExactStage,
		PartialFailureSourceFuzzyStage,
		PartialFailureSourceLocalSearch,
		PartialFailureSourceLocalEvidence,
		PartialFailureSourceWeb,
		"c0f8c1a2b3d4",
	} {
		lost := Response{PartialFailures: []PartialFailure{{Source: source}}}
		if got := lost.LostSourceFailures(); got != 1 {
			t.Fatalf("lost sources for %q = %d, want 1", source, got)
		}
	}

	mixed := Response{PartialFailures: []PartialFailure{
		{Source: PartialFailureSourceQueryShape},
		{Source: PartialFailureSourceRemoteYaCy},
	}}
	if got := mixed.LostSourceFailures(); got != 1 {
		t.Fatalf("mixed lost sources = %d, want only the remote peer counted", got)
	}
}

// TestQueryShapeSourceReadsAsAQueryProperty keeps the raw identifier off human
// surfaces. Every other source names a component an operator can inspect, so
// printing "query-shape" in the portal, the admin search page or the explain
// console would invent a subsystem that does not exist.
func TestQueryShapeSourceReadsAsAQueryProperty(t *testing.T) {
	t.Parallel()

	shaped := PartialFailure{Source: PartialFailureSourceQueryShape}
	if label := shaped.SourceLabel(); label != "query without search words" {
		t.Fatalf("label = %q, want a phrase describing the query", label)
	}
	if label := (PartialFailure{Source: PartialFailureSourceWeb}).SourceLabel(); label != "web" {
		t.Fatalf("web label = %q", label)
	}
	if label := (PartialFailure{
		Source: PartialFailureSourceRemoteYaCy,
	}).SourceLabel(); label != PartialFailureSourceRemoteYaCy {
		t.Fatalf("unmapped label = %q, want the source verbatim", label)
	}
}

// TestUnprovenZeroIgnoresQueryShapeFailures carries the same distinction into
// the predicate every public surface reads.
func TestUnprovenZeroIgnoresQueryShapeFailures(t *testing.T) {
	t.Parallel()

	settled := ResultAvailability{Exhausted: true}
	shaped := Response{
		Availability:    settled,
		PartialFailures: []PartialFailure{{Source: PartialFailureSourceQueryShape}},
	}
	if shaped.UnprovenZero() {
		t.Fatal("a query the DHT could not be asked is a settled zero, not an unproven one")
	}

	lost := Response{
		Availability:    settled,
		PartialFailures: []PartialFailure{{Source: PartialFailureSourceRemoteYaCy}},
	}
	if !lost.UnprovenZero() {
		t.Fatal("a genuinely lost peer must still make an empty answer unproven")
	}

	// Availability that never settled is still enough on its own, with no
	// failure of any kind.
	if !(Response{}).UnprovenZero() {
		t.Fatal("an unsettled availability must still make an empty answer unproven")
	}
	if (Response{
		Availability: settled,
		Results:      []Result{{URL: "https://example.org/doc"}},
	}).UnprovenZero() {
		t.Fatal("an answered search is never an unproven zero")
	}
}
