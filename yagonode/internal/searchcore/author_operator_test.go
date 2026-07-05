package searchcore

import "testing"

func TestParseTextQueryAuthorOperator(t *testing.T) {
	parsed := ParseTextQuery(`author:Doe golang`)
	if parsed.Author != "Doe" {
		t.Fatalf("author = %q, want Doe", parsed.Author)
	}
	if len(parsed.Terms) != 1 || parsed.Terms[0] != "golang" {
		t.Fatalf("terms = %v, want the operator stripped", parsed.Terms)
	}
}
