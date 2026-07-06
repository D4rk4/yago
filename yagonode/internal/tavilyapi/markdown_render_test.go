package tavilyapi

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestDocumentMarkdownRendersStructure(t *testing.T) {
	doc := documentstore.Document{
		Title:    "Guide",
		Headings: []string{"Install", "Usage"},
		ExtractedText: "Intro paragraph here.\nInstall\n- step one\n* step two\n" +
			"Usage\nRun the binary.\n\n",
	}
	got := documentMarkdown(doc)
	for _, want := range []string{
		"# Guide", "## Install", "## Usage", "- step one", "* step two",
		"Intro paragraph here.", "Run the binary.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("markdown has blank runs:\n%s", got)
	}

	// Titleless documents render their text alone.
	if got := documentMarkdown(
		documentstore.Document{ExtractedText: "just text"},
	); got != "just text" {
		t.Fatalf("titleless = %q", got)
	}
	// Heading entries that are blank are ignored.
	blank := documentMarkdown(documentstore.Document{
		Title: "T", Headings: []string{"  "}, ExtractedText: "body",
	})
	if strings.Contains(blank, "##") {
		t.Fatalf("blank heading rendered: %q", blank)
	}
}

func TestRelevantChunks(t *testing.T) {
	text := "Alpha covers installation basics. Beta explains removal. " +
		"Gamma covers installation of plugins. Delta is unrelated filler text."
	got := relevantChunks(text, []string{"installation"}, 2)
	if !strings.Contains(got, "Alpha covers installation basics.") ||
		!strings.Contains(got, "Gamma covers installation of plugins.") {
		t.Fatalf("chunks = %q", got)
	}
	if !strings.Contains(got, " [...] ") {
		t.Fatalf("chunk separator missing: %q", got)
	}
	if strings.Contains(got, "Delta") {
		t.Fatalf("irrelevant chunk kept: %q", got)
	}

	// No matches falls back to the leading snippet; zero limit is corrected.
	if got := relevantChunks(
		text,
		[]string{"zzz-none"},
		0,
	); !strings.HasPrefix(
		got,
		"Alpha covers",
	) {
		t.Fatalf("fallback = %q", got)
	}
}

func TestChunksPerSourceAndMarkdownRawInSearch(t *testing.T) {
	endpoint, _, documents := richSearchEndpoint()
	documents.rows["https://example.org/doc"] = documentstore.Document{
		Title:    "Doc title",
		Headings: []string{"Chapter"},
		ExtractedText: "Golang powers this service reliably. Unrelated body filler.\n" +
			"Chapter\nGolang tooling stays fast.",
	}
	chunks := 2
	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{
			Query:             "golang",
			SearchDepth:       "advanced",
			ChunksPerSource:   &chunks,
			IncludeRawContent: "markdown",
		},
		timeFilterClock(),
		"id-md",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("results missing")
	}
	content := resp.Results[0].Content
	if !strings.Contains(content, " [...] ") || strings.Contains(content, "Unrelated body filler") {
		t.Fatalf("chunked content = %q", content)
	}
	raw := resp.Results[0].RawContent
	if raw == nil || !strings.Contains(*raw, "# Doc title") ||
		!strings.Contains(*raw, "## Chapter") {
		t.Fatalf("markdown raw = %q", rawContent(raw))
	}
}

func TestMultiDomainWidensRetrievalAndCapsResults(t *testing.T) {
	endpoint, search, _ := richSearchEndpoint()
	one := 1
	_, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{
			Query:          "golang",
			MaxResults:     &one,
			IncludeDomains: []string{"example.org", "blocked.example"},
		},
		timeFilterClock(),
		"id-multi",
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if search.got.SiteHost != "" {
		t.Fatalf("multi-domain must not narrow retrieval: %q", search.got.SiteHost)
	}
	if search.got.Limit != one*domainOverfetchFactor {
		t.Fatalf("overfetch limit = %d", search.got.Limit)
	}

	// Single-domain requests keep narrowing retrieval.
	_, err = endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", IncludeDomains: []string{"example.org"}},
		timeFilterClock(),
		"id-single",
	)
	if err != nil {
		t.Fatalf("single search: %v", err)
	}
	if search.got.SiteHost != "example.org" {
		t.Fatalf("single domain host = %q", search.got.SiteHost)
	}

	// The response is capped at max_results even when overfetch returned more.
	search.response.Results = append(search.response.Results, search.response.Results[0])
	resp, err := endpoint.searchResponse(
		t.Context(),
		SearchRequest{Query: "golang", MaxResults: &one},
		timeFilterClock(),
		"id-cap",
	)
	if err != nil {
		t.Fatalf("cap search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("capped results = %d", len(resp.Results))
	}
}

func TestResponseResultsRejectsBadLimit(t *testing.T) {
	endpoint, _, _ := richSearchEndpoint()
	bad := -1
	if _, _, err := endpoint.responseResults(
		t.Context(),
		SearchRequest{Query: "golang", MaxResults: &bad},
		searchcore.Request{Query: "golang"},
		nil,
	); err == nil {
		t.Fatal("negative max_results must fail")
	}
}
