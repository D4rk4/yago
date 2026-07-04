package extractfetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAppendCollapsedIgnoresBlankInput(t *testing.T) {
	var builder strings.Builder
	appendCollapsed(&builder, "   \t\n ")
	if builder.Len() != 0 {
		t.Fatalf("builder = %q, want empty for a whitespace-only input", builder.String())
	}
}

func TestIsHTMLTreatsEmptyContentTypeAsHTML(t *testing.T) {
	if !isHTML("") {
		t.Fatal("an empty content type should be treated as HTML")
	}
}

func TestFetcherRejectsUnbuildableRequest(t *testing.T) {
	_, err := New(nil, 0, 0).Fetch(context.Background(), "http://\x7f/bad")
	if err == nil {
		t.Fatal("expected a build-request error for an invalid URL")
	}
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial failed")
}

func TestFetcherReportsTransportError(t *testing.T) {
	client := &http.Client{Transport: errRoundTripper{}}
	_, err := New(client, time.Second, 0).Fetch(context.Background(), "https://example.com/")
	if err == nil {
		t.Fatal("expected a transport error from the client")
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

func TestFetcherReportsParseError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/html")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errReadCloser{}),
			Header:     header,
		}, nil
	})}
	_, err := New(client, 0, 0).Fetch(context.Background(), "https://example.com/")
	if err == nil {
		t.Fatal("expected a parse error when the body read fails")
	}
}
