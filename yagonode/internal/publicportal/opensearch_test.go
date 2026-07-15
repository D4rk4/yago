package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenSearchDescribe(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://node.example/opensearch.xml",
		nil,
	)
	NewOpenSearch().Describe(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != osddContentType {
		t.Fatalf("content-type = %q, want %q", ct, osddContentType)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<?xml",
		`xmlns="` + osddNamespace + `"`,
		"<ShortName>yago search</ShortName>",
		`template="http://node.example/?q={searchTerms}"`,
		`template="http://node.example/opensearch/suggest?q={searchTerms}"`,
		"AGPL",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("descriptor missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `xmlns=""`) {
		t.Errorf("child element reset the namespace to empty\n%s", body)
	}
}

func TestOpenSearchDescribeHonoursForwardedProto(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://node.example/opensearch.xml",
		nil,
	)
	req.Header.Set("X-Forwarded-Proto", "https")
	NewOpenSearch().Describe(rec, req)

	if !strings.Contains(rec.Body.String(), `template="https://node.example/?q={searchTerms}"`) {
		t.Fatalf("expected an https template, got:\n%s", rec.Body.String())
	}
}

func TestOpenSearchSuggestReturnsEmptyCompletions(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://node.example/opensearch/suggest?q=cats",
		nil,
	)
	NewOpenSearch().Suggest(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != suggestionsType {
		t.Fatalf("content-type = %q, want %q", ct, suggestionsType)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `["cats",[]]` {
		t.Fatalf("suggestions = %q, want privacy-preserving empty completions", got)
	}
}
