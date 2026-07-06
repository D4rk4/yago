package searchcore

import "testing"

func TestResultMentionsTermsExactAndFoldedMatch(t *testing.T) {
	result := Result{Title: "BEENET — Интернет провайдер в Черногории"}
	if !ResultMentionsTerms(result, []string{"ЧЕРНОГОРИИ"}) {
		t.Fatal("case-folded exact mention not found")
	}
	if !ResultMentionsTerms(Result{Snippet: "montenegro coast"}, []string{"montenegro"}) {
		t.Fatal("snippet mention not found")
	}
}

func TestResultMentionsTermsInflectedForm(t *testing.T) {
	result := Result{Title: "Вооружённые силы Черногории — Википедия"}
	if !ResultMentionsTerms(result, []string{"черногория"}) {
		t.Fatal("inflected surface form did not verify")
	}
}

func TestResultMentionsTermsPercentEncodedURL(t *testing.T) {
	result := Result{
		Title: "Википедия",
		URL:   "https://ru.wikipedia.org/wiki/%D0%A7%D0%B5%D1%80%D0%BD%D0%BE%D0%B3%D0%BE%D1%80%D0%B8%D1%8F",
	}
	if !ResultMentionsTerms(result, []string{"черногория"}) {
		t.Fatal("percent-encoded URL mention not found")
	}
}

func TestResultMentionsTermsUndecodableURLUsedRaw(t *testing.T) {
	result := Result{URL: "https://example.org/a%ZZb-golang"}
	if !ResultMentionsTerms(result, []string{"golang"}) {
		t.Fatal("raw URL mention not found after decode failure")
	}
}

func TestResultMentionsTermsSubstringServesUnsegmentedScripts(t *testing.T) {
	result := Result{Title: "東京タワーの案内"}
	if !ResultMentionsTerms(result, []string{"東京"}) {
		t.Fatal("CJK substring mention not found")
	}
}

func TestResultMentionsTermsRejectsUnrelatedResult(t *testing.T) {
	result := Result{
		Title:   "XXX Видео с Моделями",
		Snippet: "Unantastbar - Punkrock aus Südtirol",
		URL:     "https://rt.xgroovy.com/pornstars/",
	}
	if ResultMentionsTerms(result, []string{"черногория", "православие"}) {
		t.Fatal("unrelated result verified")
	}
}

func TestResultMentionsTermsShortSharedPrefixIsNotEnough(t *testing.T) {
	if ResultMentionsTerms(Result{Title: "чертёж дома"}, []string{"черногория"}) {
		t.Fatal("a short shared prefix must not count as a mention")
	}
}

func TestResultMentionsTermsEmptyAndBlankTerms(t *testing.T) {
	if !ResultMentionsTerms(Result{Title: "anything"}, nil) {
		t.Fatal("no terms means nothing to check")
	}
	if ResultMentionsTerms(Result{Title: "anything"}, []string{"  "}) {
		t.Fatal("a blank term must not verify a result")
	}
}
