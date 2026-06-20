package contracts

import "testing"

func TestParseSearchModifierExtractsTokens(t *testing.T) {
	got := ParseSearchModifier(
		"site:example.com /language/DE /https filetype:pdf author:(John Doe) keyword:go collection:c",
	)
	want := SearchModifier{
		Language:   "de",
		SiteHost:   "example.com",
		Protocol:   "https",
		FileType:   "pdf",
		Author:     "John",
		Keyword:    "go",
		Collection: "c",
	}
	if got != want {
		t.Fatalf("parsed = %+v, want %+v", got, want)
	}
}

func TestParseSearchModifierIgnoresInvalidLanguage(t *testing.T) {
	if got := ParseSearchModifier("/language/deu").Language; got != "" {
		t.Fatalf("language = %q, want empty", got)
	}
}

func TestJoinLanguageUsesModifierOnly(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{Language: "en", Modifier: "/language/de"}}
	if got := query.JoinLanguage(); got != "de" {
		t.Fatalf("join language = %q, want de", got)
	}
}

func TestJoinLanguageIgnoresStandaloneLanguage(t *testing.T) {
	query := SearchQuery{Filters: SearchFilters{Language: "en", Modifier: "filetype:pdf"}}
	if got := query.JoinLanguage(); got != "" {
		t.Fatalf("join language = %q, want empty", got)
	}
}
