package searchcore

import "testing"

func TestParseTextQueryExtractsTermsAndOperators(t *testing.T) {
	got := ParseTextQuery(
		`golang "p2p search" -java LANGUAGE:en site:yacy.net inurl:docs tld:org filetype:pdf NEAR /date <script>`,
	)

	if got.Language != "en" ||
		got.SiteHost != "yacy.net" ||
		got.InURL != "docs" ||
		got.TLD != "org" ||
		got.FileType != "pdf" ||
		!got.Near ||
		!got.SortByDate {
		t.Fatalf("operators = %#v", got)
	}
	assertStrings(t, got.Terms, []string{"golang", "p2p", "search", "script"})
	assertStrings(t, got.ExcludedTerms, []string{"java"})
}

func TestParseTextQueryAcceptsSlashLanguage(t *testing.T) {
	got := ParseTextQuery(`- /language/de frei`)
	if got.Language != "de" || len(got.Terms) != 1 || got.Terms[0] != "frei" {
		t.Fatalf("parsed = %#v", got)
	}
}

func TestNormalizePublicRequestDefaultsAndCaps(t *testing.T) {
	got, err := NormalizePublicRequest(Request{Limit: 50}, 7)
	if err != nil {
		t.Fatalf("NormalizePublicRequest: %v", err)
	}
	if got.Source != SourceGlobal ||
		got.ContentDomain != ContentDomainText ||
		got.Verify != VerifyIfExist ||
		got.Limit != 7 {
		t.Fatalf("normalized = %#v", got)
	}
}

func TestNormalizePublicRequestUsesDefaultCap(t *testing.T) {
	got, err := NormalizePublicRequest(Request{
		Limit:            50,
		URLMaskFilter:    ".*",
		PreferMaskFilter: "docs",
	}, 0)
	if err != nil {
		t.Fatalf("NormalizePublicRequest: %v", err)
	}
	if got.Limit != DefaultPublicLimit {
		t.Fatalf("Limit = %d, want default cap", got.Limit)
	}
}

func TestNormalizePublicRequestRejectsInvalidValues(t *testing.T) {
	cases := []Request{
		{Offset: -1},
		{Source: Source("remote")},
		{ContentDomain: ContentDomain("book")},
		{Verify: VerifyMode("online")},
		{URLMaskFilter: "["},
		{PreferMaskFilter: "["},
	}
	for _, req := range cases {
		if _, err := NormalizePublicRequest(req, 10); err == nil {
			t.Fatalf("NormalizePublicRequest(%#v) succeeded", req)
		}
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d = %q, want %q; got %#v", i, got[i], want[i], got)
		}
	}
}

func TestParsedQueryPhrasesReturnsQuotedMultiWord(t *testing.T) {
	got := ParseTextQuery(`golang "p2p search" tooling`)
	assertStrings(t, got.Phrases(), []string{"p2p search"})
}

func TestParsedQueryPhrasesIgnoresSingleWords(t *testing.T) {
	if phrases := ParseTextQuery("golang p2p search").Phrases(); len(phrases) != 0 {
		t.Fatalf("phrases = %#v, want none", phrases)
	}
}
