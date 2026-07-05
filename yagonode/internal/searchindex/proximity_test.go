package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestTermsNearWindow(t *testing.T) {
	text := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu"
	if !termsNear(text, []string{"alpha", "gamma"}) {
		t.Fatal("terms three tokens apart must be near within an 8-token window")
	}
	if termsNear(text, []string{"alpha", "lambda"}) {
		t.Fatal("terms eleven tokens apart must not be near")
	}
	if !termsNear(text, nil) {
		t.Fatal("no terms means no proximity constraint")
	}
	if termsNear(text, []string{"alpha", "missing"}) {
		t.Fatal("an absent term can never be near")
	}
	if !termsNear("Go, go; GO!", []string{"go", "go"}) {
		t.Fatal("repeated terms must each need their own occurrence")
	}
	if termsNear("go once", []string{"go", "go"}) {
		t.Fatal("a single occurrence cannot satisfy a repeated term")
	}
}

func TestAllowsDocumentAuthorAndNear(t *testing.T) {
	doc := documentstore.Document{
		ExtractedText: "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda",
		Metadata:      map[string]string{"author": "Jane Q. Doe"},
	}
	if !allowsDocument(doc, SearchRequest{Author: "jane"}) {
		t.Fatal("case-insensitive author substring must match")
	}
	if allowsDocument(doc, SearchRequest{Author: "smith"}) {
		t.Fatal("non-matching author admitted")
	}
	if !allowsDocument(doc, SearchRequest{Near: true, Terms: []string{"alpha", "beta"}}) {
		t.Fatal("adjacent terms must pass the near filter")
	}
	if allowsDocument(doc, SearchRequest{Near: true, Terms: []string{"alpha", "lambda"}}) {
		t.Fatal("distant terms admitted by the near filter")
	}
}
