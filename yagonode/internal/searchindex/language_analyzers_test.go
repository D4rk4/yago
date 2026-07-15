package searchindex

import (
	"errors"
	"testing"
	"unicode"

	"github.com/blevesearch/bleve/v2/mapping"
)

func TestLanguageToAnalyzer(t *testing.T) {
	cases := map[string]string{
		"ru":      "ru",
		"RU":      "ru",
		"ru-RU":   "ru",
		"de_DE":   "de",
		"sr":      "hr",
		"bs":      "hr",
		"zh":      "cjk",
		"ja":      "cjk",
		"ko":      "cjk",
		"en":      searchTextAnalyzer,
		"ar":      "ar",
		"he":      standardTextAnalyzer,
		"":        standardTextAnalyzer,
		"unknown": standardTextAnalyzer,
	}
	for code, want := range cases {
		if got := languageToAnalyzer(code); got != want {
			t.Errorf("languageToAnalyzer(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestDetectDocumentAnalyzer(t *testing.T) {
	// Reliable content detection wins.
	if got := detectDocumentAnalyzer(
		"Черногория — государство на Балканском полуострове, столица Подгорица.", "",
	); got != "ru" {
		t.Fatalf("reliable russian = %q", got)
	}
	// Unreliable content but a usable HTML lang hint.
	if got := detectDocumentAnalyzer("x", "de"); got != "de" {
		t.Fatalf("html lang hint = %q", got)
	}
	// Unreliable and no hint: fall back to the dominant script's analyzer.
	if got := detectDocumentAnalyzer("مرحبا", ""); got != "ar" {
		t.Fatalf("script fallback (arabic) = %q", got)
	}
	// No letters at all and no hint: standard.
	if got := detectDocumentAnalyzer("123 456", ""); got != standardTextAnalyzer {
		t.Fatalf("no-signal fallback = %q", got)
	}
	// An HTML hint that names an unrouted language does not override the script.
	if got := detectDocumentAnalyzer("Привет мир", "he"); got != "ru" {
		t.Fatalf("cyrillic with unrouted hint = %q", got)
	}
}

func TestAnalyzerFromLangHint(t *testing.T) {
	if _, ok := analyzerFromLangHint(""); ok {
		t.Fatal("empty hint must not resolve")
	}
	if _, ok := analyzerFromLangHint("he"); ok {
		t.Fatal("unrouted hint must not resolve")
	}
	if analyzer, ok := analyzerFromLangHint("fr"); !ok || analyzer != "fr" {
		t.Fatalf("fr hint = %q %v", analyzer, ok)
	}
}

func TestQueryAnalyzers(t *testing.T) {
	// Cyrillic query: ru plus the always-present standard analyzer.
	if got := queryAnalyzers("черногория"); len(got) != 2 ||
		got[0] != "ru" || got[len(got)-1] != standardTextAnalyzer {
		t.Fatalf("cyrillic query analyzers = %v", got)
	}
	// Latin query fans out over the common Latin analyzers, standard last.
	latin := queryAnalyzers("running")
	if latin[0] != searchTextAnalyzer || latin[len(latin)-1] != standardTextAnalyzer {
		t.Fatalf("latin query analyzers = %v", latin)
	}
	// Hebrew has no analyzer: only the standard analyzer.
	if got := queryAnalyzers("שלום"); len(got) != 1 || got[0] != standardTextAnalyzer {
		t.Fatalf("hebrew query analyzers = %v", got)
	}
	// Empty query: standard only, no duplicates.
	if got := queryAnalyzers(""); len(got) != 1 || got[0] != standardTextAnalyzer {
		t.Fatalf("empty query analyzers = %v", got)
	}
}

func TestScriptAnalyzers(t *testing.T) {
	cases := map[*unicode.RangeTable][]string{
		unicode.Cyrillic:   {"ru"},
		unicode.Arabic:     {"ar", "fa", "ckb"},
		unicode.Han:        {"cjk"},
		unicode.Hiragana:   {"cjk"},
		unicode.Devanagari: {"hi"},
		unicode.Hebrew:     nil,
	}
	for script, want := range cases {
		got := scriptAnalyzers(script)
		if len(got) != len(want) {
			t.Fatalf("scriptAnalyzers = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("scriptAnalyzers = %v, want %v", got, want)
			}
		}
	}
}

func TestDominantScript(t *testing.T) {
	if got := dominantScript("привет мир hello"); got != unicode.Cyrillic {
		t.Fatal("mostly-cyrillic text should resolve to Cyrillic")
	}
	if got := dominantScript("hello world привет"); got != unicode.Latin {
		t.Fatal("mostly-latin text should resolve to Latin")
	}
	if got := dominantScript("12345 !@#"); got != nil {
		t.Fatalf("scriptless text = %v, want nil", got)
	}
}

func TestRegisterStandardTextAnalyzerRejectsDuplicate(t *testing.T) {
	old := registerStandardTextAnalyzer
	t.Cleanup(func() { registerStandardTextAnalyzer = old })
	sentinel := errors.New("standard analyzer register failed")
	registerStandardTextAnalyzer = func(*mapping.IndexMappingImpl) error { return sentinel }

	if _, err := newSearchIndexMapping(); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}
