package tavilyapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type extractLookupDocuments struct {
	rows     map[string]documentstore.Document
	failures map[string]error
}

func (d *extractLookupDocuments) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if err := d.failures[normalizedURL]; err != nil {
		return documentstore.Document{}, false, err
	}
	document, found := d.rows[normalizedURL]

	return document, found, nil
}

func (d *extractLookupDocuments) Count(context.Context) (int, error) {
	return len(d.rows), nil
}

type waitingExtractLookupDocuments struct {
	finished chan error
}

func (d waitingExtractLookupDocuments) Document(
	ctx context.Context,
	_ string,
) (documentstore.Document, bool, error) {
	<-ctx.Done()
	d.finished <- ctx.Err()

	return documentstore.Document{}, false, fmt.Errorf("document wait: %w", ctx.Err())
}

func (waitingExtractLookupDocuments) Count(context.Context) (int, error) {
	return 0, nil
}

type extractLookupBudgetFetcher struct {
	contextError error
	gotURL       string
	err          error
}

func (f *extractLookupBudgetFetcher) Fetch(
	ctx context.Context,
	url string,
) (FetchedContent, error) {
	f.contextError = ctx.Err()
	f.gotURL = url

	return FetchedContent{Title: "Fetched", Text: "fresh content"}, f.err
}

func TestExtractDocumentLookupDeadlinePreservesPartialResponse(t *testing.T) {
	const (
		foundURL     = "https://indexed.example/page"
		contendedURL = "https://contended.example/page"
		expiredURL   = "https://pending.example/page"
	)
	documents := &extractLookupDocuments{
		rows: map[string]documentstore.Document{
			foundURL: {ExtractedText: "indexed content"},
		},
		failures: map[string]error{
			contendedURL: fmt.Errorf("wait for URL boundary: %w", context.DeadlineExceeded),
			expiredURL:   fmt.Errorf("expired request: %w", context.DeadlineExceeded),
		},
	}
	handler := NewExtractEndpointWithAccess(
		documents,
		SearchAccessPolicy{BearerToken: extractTestKey},
	)
	recorder := postExtract(
		t,
		handler,
		`{"urls":["`+foundURL+`","`+contendedURL+`","`+expiredURL+`"]}`,
		extractTestKey,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeExtract(t, recorder)
	if len(response.Results) != 1 || response.Results[0].URL != foundURL {
		t.Fatalf("results = %#v", response.Results)
	}
	if len(response.FailedResults) != 2 {
		t.Fatalf("failed_results = %#v", response.FailedResults)
	}
	for index, expectedURL := range []string{contendedURL, expiredURL} {
		failure := response.FailedResults[index]
		if failure.URL != expectedURL || failure.Error != extractTimeoutFailureMessage {
			t.Fatalf("failure[%d] = %#v", index, failure)
		}
	}
}

func TestExtractWorkDeadlineReturnsLookupFailure(t *testing.T) {
	finished := make(chan error, 1)
	fetcher := &extractLookupBudgetFetcher{}
	endpoint := extractEndpoint{
		documents:    waitingExtractLookupDocuments{finished: finished},
		access:       SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher:      fetcher,
		now:          time.Now,
		workDuration: time.Millisecond,
	}
	recorder := postExtract(
		t,
		endpoint,
		`{"urls":"https://contended.example/page"}`,
		extractTestKey,
	)
	if recorder.Code != http.StatusOK || !errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeExtract(t, recorder)
	if len(response.Results) != 0 || len(response.FailedResults) != 1 ||
		response.FailedResults[0].Error != extractTimeoutFailureMessage {
		t.Fatalf("response = %#v", response)
	}
	if fetcher.gotURL != "" {
		t.Fatalf("fetch called after parent deadline for %q", fetcher.gotURL)
	}
}

func TestExtractLookupDeadlineContinuesWithRemainingFetchBudget(t *testing.T) {
	finished := make(chan error, 1)
	fetcher := &extractLookupBudgetFetcher{}
	endpoint := extractEndpoint{
		documents:              waitingExtractLookupDocuments{finished: finished},
		access:                 SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher:                fetcher,
		now:                    time.Now,
		workDuration:           maximumRawContentWorkDuration,
		documentLookupDuration: time.Millisecond,
	}
	recorder := postExtract(
		t,
		endpoint,
		`{"urls":"https://contended.example/page"}`,
		extractTestKey,
	)
	if recorder.Code != http.StatusOK || !errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeExtract(t, recorder)
	if len(response.Results) != 1 || len(response.FailedResults) != 0 ||
		response.Results[0].RawContent != "# Fetched\n\nfresh content" {
		t.Fatalf("response = %#v", response)
	}
	if fetcher.gotURL != "https://contended.example/page" || fetcher.contextError != nil {
		t.Fatalf("fetch URL = %q, context error = %v", fetcher.gotURL, fetcher.contextError)
	}
}

func TestExtractLookupDeadlineReportsFallbackFetchFailure(t *testing.T) {
	finished := make(chan error, 1)
	fetcher := &extractLookupBudgetFetcher{err: errors.New("fetch failed")}
	endpoint := extractEndpoint{
		documents:              waitingExtractLookupDocuments{finished: finished},
		access:                 SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher:                fetcher,
		now:                    time.Now,
		workDuration:           maximumRawContentWorkDuration,
		documentLookupDuration: time.Millisecond,
	}
	recorder := postExtract(
		t,
		endpoint,
		`{"urls":"https://contended.example/page"}`,
		extractTestKey,
	)
	if recorder.Code != http.StatusOK || !errors.Is(<-finished, context.DeadlineExceeded) {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeExtract(t, recorder)
	if len(response.Results) != 0 || len(response.FailedResults) != 1 ||
		response.FailedResults[0].Error != extractFetchFailureMessage {
		t.Fatalf("response = %#v", response)
	}
}

func TestExtractDocumentLookupDurationIsBounded(t *testing.T) {
	tests := []struct {
		requested time.Duration
		want      time.Duration
	}{
		{requested: 0, want: maximumExtractDocumentLookupDuration},
		{requested: time.Millisecond, want: time.Millisecond},
		{requested: time.Second, want: maximumExtractDocumentLookupDuration},
	}
	for _, test := range tests {
		if got := extractDocumentLookupDuration(test.requested); got != test.want {
			t.Fatalf("duration for %v = %v, want %v", test.requested, got, test.want)
		}
	}
}
