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

func TestVisibleURLTermsDecodePercentEncodedText(t *testing.T) {
	text := NewVisibleURLTerms(
		"https://ru.example/%D0%BF%D0%BE%D0%BB%D0%BD%D0%BE%D0%BC%D0%BE%D1%87%D0%B8%D0%B9",
	)
	if !text.Mentions("полномочия") {
		t.Fatal("decoded URL inflection was not found")
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

func TestResultMentionsTermsRejectsUnsegmentedPrefixAffinity(t *testing.T) {
	result := Result{Title: "東京都庁内"}
	if ResultMentionsTerms(result, []string{"東京都庁舎"}) {
		t.Fatal("unsegmented prefix affinity counted as a literal term")
	}
}

func TestResultMentionsTermsUsesLiteralIdentifierBoundaries(t *testing.T) {
	if !ResultMentionsTerms(Result{Title: "SpaceXAI’s node.js v0.0.9"}, []string{"node.js"}) {
		t.Fatal("punctuated identifier mention not found")
	}
	if ResultMentionsTerms(Result{Title: "Capital markets"}, []string{"api"}) {
		t.Fatal("embedded Latin substring counted as a term")
	}
	if ResultMentionsTerms(Result{Title: "Node server with node.jsp"}, []string{"node.js"}) {
		t.Fatal("partial punctuated identifier counted as a term")
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

func TestVisibleTextTermsOverPlainText(t *testing.T) {
	body := "Самая известная песня группы — «Что такое осень», записанная для альбома."
	visible := NewVisibleTextTerms(body)
	if !visible.Mentions("осень") || !visible.Mentions("альбома") {
		t.Fatal("plain-text mention missed")
	}
	if visible.Mentions("черногория") {
		t.Fatal("absent term matched")
	}
}
