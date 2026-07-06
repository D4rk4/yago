package stopwords

import "testing"

func TestIsStopwordAcrossLanguages(t *testing.T) {
	for _, word := range []string{"что", "такое", "ЧТО", " и ", "the", "der", "les", "los"} {
		if !IsStopword(word) {
			t.Fatalf("IsStopword(%q) = false", word)
		}
	}
	for _, word := range []string{"осень", "черногория", "autumn", "kubernetes", ""} {
		if IsStopword(word) {
			t.Fatalf("IsStopword(%q) = true", word)
		}
	}
}

func TestContentTermsKeepsMeaningBearingWords(t *testing.T) {
	got := ContentTerms([]string{"что", "такое", "осень", ""})
	if len(got) != 1 || got[0] != "осень" {
		t.Fatalf("content terms = %#v", got)
	}
	if got := ContentTerms([]string{"что", "и", "the"}); len(got) != 0 {
		t.Fatalf("all-stopword query yielded %#v", got)
	}
}
