package searchcore

import (
	"slices"
	"testing"
)

func TestNormalizeTextQuerySeparatesCompoundDashes(t *testing.T) {
	raw := `гном-гномыч "state-of-the-art" -legacy-system site:foo-bar.example author:Doe-Smith`
	want := `гном гномыч "state of the art" -legacy -system site:foo-bar.example author:Doe-Smith`
	if got := NormalizeTextQuery(raw); got != want {
		t.Fatalf("normalized query = %q, want %q", got, want)
	}
}

func TestParseTextQuerySeparatesUnicodeDashes(t *testing.T) {
	parsed := ParseTextQuery(`гном-гномыч альфа—бета -legacy–system`)
	if want := []string{"гном", "гномыч", "альфа", "бета"}; !slices.Equal(parsed.Terms, want) {
		t.Fatalf("terms = %q, want %q", parsed.Terms, want)
	}
	if want := []string{"legacy", "system"}; !slices.Equal(parsed.ExcludedTerms, want) {
		t.Fatalf("excluded terms = %q, want %q", parsed.ExcludedTerms, want)
	}
}

func TestParseTextQueryDoesNotPromoteCompoundWordsToOperators(t *testing.T) {
	parsed := ParseTextQuery(`near-death experience -near-failure`)
	if parsed.Near {
		t.Fatal("compound near word became a query operator")
	}
	if want := []string{"near", "death", "experience"}; !slices.Equal(parsed.Terms, want) {
		t.Fatalf("terms = %q, want %q", parsed.Terms, want)
	}
	if want := []string{"near", "failure"}; !slices.Equal(parsed.ExcludedTerms, want) {
		t.Fatalf("excluded terms = %q, want %q", parsed.ExcludedTerms, want)
	}
}

func TestNormalizeTextQueryKeepsCompoundNegationAcrossApostrophe(t *testing.T) {
	if got, want := NormalizeTextQuery(`-don't-stop`), `-don't -stop`; got != want {
		t.Fatalf("normalized query = %q, want %q", got, want)
	}
}
