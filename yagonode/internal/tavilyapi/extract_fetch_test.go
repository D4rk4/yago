package tavilyapi

import (
	"context"
	"errors"
	"testing"
)

type stubContentFetcher struct {
	content FetchedContent
	err     error
	gotURL  string
}

func (s *stubContentFetcher) Fetch(_ context.Context, url string) (FetchedContent, error) {
	s.gotURL = url

	return s.content, s.err
}

func TestExtractFetchesUncachedURL(t *testing.T) {
	fetcher := &stubContentFetcher{content: FetchedContent{Title: "T", Text: "Body text"}}
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)

	resp := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"https://fresh.example/page"}`, extractTestKey),
	)
	if len(resp.Results) != 1 || len(resp.FailedResults) != 0 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
	if resp.Results[0].RawContent != "Body text" {
		t.Fatalf("raw = %q", resp.Results[0].RawContent)
	}
	if fetcher.gotURL == "" {
		t.Fatal("fetcher was not called with the normalized URL")
	}
}

func TestExtractFetchFailureBecomesFailedResult(t *testing.T) {
	fetcher := &stubContentFetcher{err: errors.New("boom")}
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)

	resp := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"https://fresh.example/"}`, extractTestKey),
	)
	if len(resp.Results) != 0 || len(resp.FailedResults) != 1 {
		t.Fatalf("results=%d failed=%d", len(resp.Results), len(resp.FailedResults))
	}
	if resp.FailedResults[0].Error != "fetch-on-extract failed" {
		t.Fatalf("error = %q", resp.FailedResults[0].Error)
	}
}

func TestExtractWithoutFetcherReportsDisabled(t *testing.T) {
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		nil,
	)

	resp := decodeExtract(
		t,
		postExtract(t, handler, `{"urls":"https://fresh.example/"}`, extractTestKey),
	)
	if len(resp.FailedResults) != 1 ||
		resp.FailedResults[0].Error != "url is not in the index and fetch-on-extract is disabled" {
		t.Fatalf("failed = %#v", resp.FailedResults)
	}
}

func TestExtractFetchMarkdown(t *testing.T) {
	fetcher := &stubContentFetcher{content: FetchedContent{Title: "T", Text: "Body"}}
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)

	resp := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"https://fresh.example/","format":"markdown"}`,
			extractTestKey,
		),
	)
	if resp.Results[0].RawContent != "# T\n\nBody" {
		t.Fatalf("raw = %q", resp.Results[0].RawContent)
	}
}

func TestExtractFetchMarkdownWithoutTitle(t *testing.T) {
	fetcher := &stubContentFetcher{content: FetchedContent{Text: "Body only"}}
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)

	resp := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"https://fresh.example/","format":"markdown"}`,
			extractTestKey,
		),
	)
	if resp.Results[0].RawContent != "Body only" {
		t.Fatalf("raw = %q, want the untitled body verbatim", resp.Results[0].RawContent)
	}
}

func TestExtractFetchIncludesFavicon(t *testing.T) {
	fetcher := &stubContentFetcher{content: FetchedContent{Title: "T", Text: "Body"}}
	handler := NewExtractEndpointWithFetcher(
		&fakeDocuments{},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)

	resp := decodeExtract(
		t,
		postExtract(
			t,
			handler,
			`{"urls":"https://fresh.example/page","include_favicon":true}`,
			extractTestKey,
		),
	)
	if len(resp.Results) != 1 ||
		resp.Results[0].Favicon != "https://fresh.example/favicon.ico" {
		t.Fatalf("favicon = %#v, want the derived favicon URL", resp.Results)
	}
}
