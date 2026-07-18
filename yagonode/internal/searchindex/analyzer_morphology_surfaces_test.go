package searchindex

import (
	"slices"
	"strings"
	"testing"
)

func TestGeneratedMorphologySurfacesFindsMultilingualSiblingInflections(t *testing.T) {
	fixtures := []struct {
		source  string
		sibling string
	}{
		{source: "house", sibling: "houses"},
		{source: "häuser", sibling: "häusern"},
		{source: "kind", sibling: "kinder"},
		{source: "الكتابات", sibling: "الكتابان"},
		{source: "полномочия", sibling: "полномочий"},
		{source: "чрезвычайные", sibling: "чрезвычайных"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.source, func(t *testing.T) {
			got := GeneratedMorphologySurfaces(fixture.source)
			if !slices.Contains(got, fixture.sibling) {
				t.Fatalf("generated surfaces = %v; missing %q", got, fixture.sibling)
			}
			if len(got) > maximumGeneratedMorphologySurfaces || got[0] != fixture.source {
				t.Fatalf("generated surface boundary = %v", got)
			}
			if slices.Index(got, fixture.sibling) >= 10 {
				t.Fatalf("sibling falls outside the live two-term surface bound: %v", got)
			}
		})
	}
}

func TestAnalyzerMorphologyRulesMatchRegisteredSnowballAnalyzers(t *testing.T) {
	for _, analyzer := range []string{
		"ar", "da", "de", "en", "es", "fi", "fr", "hu", "it", "nl", "no",
		"pt", "ro", "ru", "sv", "tr",
	} {
		if len(analyzerMorphologyRules(analyzer)) == 0 {
			t.Fatalf("missing morphology rules for %q", analyzer)
		}
	}
	if analyzerMorphologyRules("he") != nil {
		t.Fatal("unsupported analyzer has morphology rules")
	}
}

func TestGeneratedMorphologySurfacesBoundaries(t *testing.T) {
	if got := GeneratedMorphologySurfaces("   "); got != nil {
		t.Fatalf("blank surfaces = %v", got)
	}
	if got := GeneratedMorphologySurfaces("CPU"); !slices.Equal(got, []string{"cpu"}) {
		t.Fatalf("short surfaces = %v", got)
	}
	longWord := strings.Repeat("a", maximumGeneratedMorphologyWordRunes+1)
	if got := GeneratedMorphologySurfaces(longWord); !slices.Equal(got, []string{longWord}) {
		t.Fatalf("long surfaces = %v", got)
	}
	if got := GeneratedMorphologySurfaces("שלום"); !slices.Equal(got, []string{"שלום"}) {
		t.Fatalf("unsupported analyzer surfaces = %v", got)
	}
	sources := morphologyAnalyzerSources("kind")
	wantAnalyzers := []string{
		searchTextAnalyzer, "da", "de", "es", "fi", "fr", "hu", "it",
		"nl", "no", "pt", "ro", "sv", "tr",
	}
	gotAnalyzers := make([]string, len(sources))
	for position, source := range sources {
		gotAnalyzers[position] = source.analyzer
	}
	if !slices.Equal(gotAnalyzers, wantAnalyzers) {
		t.Fatalf("unchanged analyzer sources = %v, want %v", gotAnalyzers, wantAnalyzers)
	}
	if got := generatedMorphologySurfaces(
		"house",
		morphologyAnalyzerSources("house"),
		0,
	); !slices.Equal(got, []string{"house"}) {
		t.Fatalf("attempt-limited surfaces = %v", got)
	}
}

func TestMorphologyCandidateCyclePreservesAnalyzerFairness(t *testing.T) {
	sources := morphologyAnalyzerSources("kind")
	cycle := newMorphologyCandidateCycle("kind", sources)
	got := make([]string, 0, len(sources))
	for range len(sources) {
		proposal, available := cycle.next()
		if !available {
			t.Fatalf("candidate cycle ended after %d analyzers", len(got))
		}
		got = append(got, proposal.analyzer)
	}
	want := make([]string, len(sources))
	for position, source := range sources {
		want[position] = source.analyzer
	}
	if !slices.Equal(got, want) {
		t.Fatalf("candidate analyzer cycle = %v, want %v", got, want)
	}
}

func BenchmarkGeneratedMorphologySurfaces(b *testing.B) {
	for b.Loop() {
		GeneratedMorphologySurfaces("programming")
		GeneratedMorphologySurfaces("полномочия")
	}
}
