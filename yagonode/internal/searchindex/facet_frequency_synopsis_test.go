package searchindex

import (
	"runtime"
	"strings"
	"testing"
)

func TestFacetFrequencySynopsisBoundsDistinctLabelsAndKeepsHeavyHitters(t *testing.T) {
	synopsis := newFacetFrequencySynopsis(4)
	for _, term := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"} {
		synopsis.observe(term)
	}
	for range 20 {
		synopsis.observe("frequent")
	}
	if len(synopsis.entries) != 4 || len(synopsis.queue) != 4 {
		t.Fatalf("entries/queue = %d/%d", len(synopsis.entries), len(synopsis.queue))
	}
	found := false
	for _, term := range synopsis.terms() {
		if term.Term == "frequent" && term.Count == 20 {
			found = true
		}
	}
	if !found {
		t.Fatalf("heavy hitter missing: %#v", synopsis.terms())
	}
}

func TestFacetFrequencySynopsisOwnsRetainedLabels(t *testing.T) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	synopsis := synopsisWithRetainedLabel()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc+(4<<20) {
		t.Fatalf("facet label retained %d heap bytes", after.HeapAlloc-before.HeapAlloc)
	}
	label := strings.Repeat("x", facetMaxLabel)
	retained := synopsis.terms()[0].Term
	if retained != label {
		t.Fatalf("retained label = %q, want %q", retained, label)
	}
	synopsis.observe("replacement")
	if _, found := synopsis.entries[label]; found {
		t.Fatal("replaced label remains indexed")
	}
	runtime.KeepAlive(synopsis)
}

func synopsisWithRetainedLabel() *facetFrequencySynopsis {
	backing := strings.Repeat("x", 8<<20)
	label := backing[:facetMaxLabel]
	synopsis := newFacetFrequencySynopsis(1)
	synopsis.observe(label)

	return synopsis
}

func TestFacetFrequencySynopsisZeroLimit(t *testing.T) {
	synopsis := newFacetFrequencySynopsis(-1)
	synopsis.observe("ignored")
	if len(synopsis.entries) != 0 || len(synopsis.terms()) != 0 {
		t.Fatalf("zero synopsis = %#v", synopsis)
	}
}
