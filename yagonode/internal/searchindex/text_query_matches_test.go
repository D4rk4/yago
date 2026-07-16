package searchindex

import (
	"reflect"
	"testing"
)

func TestTextQueryMatchesUsesResultAnalyzer(t *testing.T) {
	text := "Получение чрезвычайных полномочий передали Путину"
	want := []string{"чрезвычайных", "полномочий", "Путину"}
	matches := NewAnalyzedQueryTerms(
		[]string{"чрезвычайные", "полномочия", "Путина"},
		"ru",
	).TextMatches(text)
	got := make([]string, len(matches))
	for index, match := range matches {
		got[index] = text[match.Start:match.End]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %q, want %q", got, want)
	}
}

func TestTextQueryMatchesRejectsUnavailableEvidence(t *testing.T) {
	if matches := NewAnalyzedQueryTerms(nil, "en").TextMatches("text"); matches != nil {
		t.Fatalf("matches = %#v, want nil", matches)
	}
}

func TestTextQueryMatchesPreservesAuthoritativeEmptyEvidence(t *testing.T) {
	for _, matches := range [][]TextQueryMatch{
		NewAnalyzedQueryTerms([]string{"term"}, "en").TextMatches(""),
		NewAnalyzedQueryTerms([]string{"the"}, "en").TextMatches("text"),
		NewAnalyzedQueryTerms([]string{"term"}, "en").TextMatches("unrelated"),
	} {
		if matches == nil || len(matches) != 0 {
			t.Fatalf("matches = %#v, want non-nil empty evidence", matches)
		}
	}
}

func TestTextQueryMatchesSkipsBlankTerms(t *testing.T) {
	matches := NewAnalyzedQueryTerms([]string{" ", "term"}, "en").TextMatches("term")
	if len(matches) != 1 || matches[0] != (TextQueryMatch{Start: 0, End: 4}) {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestTextQueryMatchesUsesCJKAnalyzerOffsets(t *testing.T) {
	text := "東京タワー"
	matches := NewAnalyzedQueryTerms([]string{"東京"}, "cjk").TextMatches(text)
	if len(matches) != 1 || text[matches[0].Start:matches[0].End] != "東京" {
		t.Fatalf("CJK matches = %#v", matches)
	}
}

func BenchmarkAnalyzedQueryTermsTextMatches(b *testing.B) {
	query := NewAnalyzedQueryTerms(
		[]string{"чрезвычайные", "полномочия", "путина"},
		"ru",
	)
	text := "Чрезвычайных полномочий передали Путину"
	b.ReportAllocs()
	for b.Loop() {
		if len(query.TextMatches(text)) != 3 {
			b.Fatal("query matches changed")
		}
	}
}
